using System.Text.Json;
using System.Text.Json.Serialization;
using Dancehub.Cart.V1;
using Grpc.Core;

public sealed class CartGrpcService(ICartStore store):CartService.CartServiceBase
{
    // Attempts before a request gives up on a cart that keeps changing under it. Each retry
    // re-reads the cart, so a losing writer never overwrites the winner's items.
    public const int MaxUpdateAttempts=16;
    // Jittered wait between attempts so contending writers spread out instead of colliding
    // again immediately; tests replace it to run instantly.
    public Func<int,CancellationToken,Task> Backoff{get;init;}=DefaultBackoff;
    public static Task DefaultBackoff(int attempt,CancellationToken token)=>Task.Delay(Random.Shared.Next(1,4<<Math.Min(attempt,5)),token);
    static string User(ServerCallContext context)=>context.RequestHeaders.GetValue("x-user-id")??throw new RpcException(new Status(StatusCode.Unauthenticated,"authenticated user metadata is required"));
    static string Key(string user)=>"dancehub:cart:"+user;
    static CartState Parse(string? value)=>value is null?new CartState():JsonSerializer.Deserialize(value,CartJsonContext.Default.CartState)??new CartState();
    static Cart View(string user,CartState state){var cart=new Cart{UserId=user,StudioId=state.StudioId??"",IssuerScope=state.IssuerScope??""};cart.Items.AddRange(state.Items.Select(item=>new CartItem{ProductVersionId=item.ProductVersionId,Quantity=item.Quantity}));return cart;}
    // Optimistic concurrency: read the cart, apply the change, and store it only if nobody
    // else wrote in between; otherwise start over from the fresh value.
    async Task<Cart> Update(string user,Action<CartState> mutate,CancellationToken token)
    {
        for(var attempt=1;;attempt++)
        {
            var current=await store.Get(Key(user),token);
            var state=Parse(current);
            mutate(state);
            var next=JsonSerializer.Serialize(state,CartJsonContext.Default.CartState);
            if(await store.CompareAndSet(Key(user),current,next,token))return View(user,state);
            if(attempt>=MaxUpdateAttempts)throw new RpcException(new Status(StatusCode.Aborted,"cart changed concurrently; retry the request"));
            await Backoff(attempt,token);
        }
    }
    public override async Task<Cart> GetCart(GetCartRequest request,ServerCallContext context){var user=User(context);return View(user,Parse(await store.Get(Key(user),context.CancellationToken)));}
    public override Task<Cart> AddItem(AddItemRequest request,ServerCallContext context)
    {
        var user=User(context);
        if(request.Item is null||string.IsNullOrWhiteSpace(request.Item.ProductVersionId)||request.Item.Quantity<1)throw new RpcException(new Status(StatusCode.InvalidArgument,"product and positive quantity are required"));
        return Update(user,state=>
        {
            if(state.Items.Count>0&&(state.StudioId!=request.StudioId||state.IssuerScope!=request.IssuerScope))throw new RpcException(new Status(StatusCode.FailedPrecondition,"cart items must share one issuer scope"));
            state.StudioId=request.StudioId;state.IssuerScope=request.IssuerScope;
            var item=state.Items.FirstOrDefault(value=>value.ProductVersionId==request.Item.ProductVersionId);
            if(item is null)state.Items.Add(new CartItemState{ProductVersionId=request.Item.ProductVersionId,Quantity=request.Item.Quantity});else item.Quantity+=request.Item.Quantity;
        },context.CancellationToken);
    }
    public override Task<Cart> RemoveItem(RemoveItemRequest request,ServerCallContext context)
    {
        var user=User(context);
        return Update(user,state=>
        {
            state.Items.RemoveAll(item=>item.ProductVersionId==request.ProductVersionId);
            if(state.Items.Count==0){state.StudioId="";state.IssuerScope="";}
        },context.CancellationToken);
    }
    public override async Task<Cart> EmptyCart(EmptyCartRequest request,ServerCallContext context){var user=User(context);await store.Remove(Key(user),context.CancellationToken);return new Cart{UserId=user};}
}

internal sealed class CartState{public string StudioId{get;set;}="";public string IssuerScope{get;set;}="";public List<CartItemState> Items{get;set;}=[];}
internal sealed class CartItemState{public string ProductVersionId{get;set;}="";public int Quantity{get;set;}}

[JsonSerializable(typeof(CartState))]
internal sealed partial class CartJsonContext:JsonSerializerContext{}
