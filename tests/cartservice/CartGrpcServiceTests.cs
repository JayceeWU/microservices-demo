using Dancehub.Cart.V1;
using Grpc.Core;
using Xunit;

namespace BayAreaDanceHub.Cart.Tests;

public sealed class CartGrpcServiceTests
{
    private static CartGrpcService NewService(InMemoryCartStore? store = null) =>
        new(store ?? new InMemoryCartStore()) { Backoff = (_, _) => Task.CompletedTask };

    private static AddItemRequest Add(string product, int quantity = 1) => new()
    {
        StudioId = "studio-1",
        IssuerScope = "STUDIO",
        Item = new CartItem { ProductVersionId = product, Quantity = quantity },
    };

    [Fact]
    public async Task GetCart_without_user_header_is_unauthenticated()
    {
        var service = NewService();
        var ex = await Assert.ThrowsAsync<RpcException>(
            () => service.GetCart(new GetCartRequest(), TestServerCallContext.Create()));
        Assert.Equal(StatusCode.Unauthenticated, ex.StatusCode);
    }

    [Fact]
    public async Task GetCart_for_new_user_is_empty()
    {
        var service = NewService();
        var cart = await service.GetCart(new GetCartRequest(), TestServerCallContext.Create("user-1"));
        Assert.Equal("user-1", cart.UserId);
        Assert.Empty(cart.Items);
    }

    [Fact]
    public async Task AddItem_rejects_missing_item()
    {
        var service = NewService();
        var request = new AddItemRequest { StudioId = "studio-1", IssuerScope = "STUDIO" };
        var ex = await Assert.ThrowsAsync<RpcException>(
            () => service.AddItem(request, TestServerCallContext.Create("user-1")));
        Assert.Equal(StatusCode.InvalidArgument, ex.StatusCode);
    }

    [Fact]
    public async Task AddItem_rejects_non_positive_quantity()
    {
        var service = NewService();
        var request = new AddItemRequest
        {
            StudioId = "studio-1",
            IssuerScope = "STUDIO",
            Item = new CartItem { ProductVersionId = "product-1", Quantity = 0 },
        };
        var ex = await Assert.ThrowsAsync<RpcException>(
            () => service.AddItem(request, TestServerCallContext.Create("user-1")));
        Assert.Equal(StatusCode.InvalidArgument, ex.StatusCode);
    }

    [Fact]
    public async Task AddItem_adds_a_new_line_and_persists_it()
    {
        var service = NewService();
        var context = TestServerCallContext.Create("user-1");
        var request = new AddItemRequest
        {
            StudioId = "studio-1",
            IssuerScope = "STUDIO",
            Item = new CartItem { ProductVersionId = "product-1", Quantity = 2 },
        };

        var cart = await service.AddItem(request, context);

        Assert.Equal("studio-1", cart.StudioId);
        var line = Assert.Single(cart.Items);
        Assert.Equal("product-1", line.ProductVersionId);
        Assert.Equal(2, line.Quantity);

        var reloaded = await service.GetCart(new GetCartRequest(), context);
        Assert.Equal(2, Assert.Single(reloaded.Items).Quantity);
    }

    [Fact]
    public async Task AddItem_increments_quantity_for_an_existing_line()
    {
        var service = NewService();
        var context = TestServerCallContext.Create("user-1");
        var request = new AddItemRequest
        {
            StudioId = "studio-1",
            IssuerScope = "STUDIO",
            Item = new CartItem { ProductVersionId = "product-1", Quantity = 2 },
        };
        await service.AddItem(request, context);

        var cart = await service.AddItem(request, context);

        var line = Assert.Single(cart.Items);
        Assert.Equal(4, line.Quantity);
    }

    [Fact]
    public async Task AddItem_rejects_mixing_issuer_scopes_in_one_cart()
    {
        var service = NewService();
        var context = TestServerCallContext.Create("user-1");
        await service.AddItem(new AddItemRequest
        {
            StudioId = "studio-1",
            IssuerScope = "STUDIO",
            Item = new CartItem { ProductVersionId = "product-1", Quantity = 1 },
        }, context);

        var conflicting = new AddItemRequest
        {
            StudioId = "studio-2",
            IssuerScope = "PLATFORM",
            Item = new CartItem { ProductVersionId = "product-2", Quantity = 1 },
        };
        var ex = await Assert.ThrowsAsync<RpcException>(() => service.AddItem(conflicting, context));
        Assert.Equal(StatusCode.FailedPrecondition, ex.StatusCode);
    }

    [Fact]
    public async Task RemoveItem_drops_the_line_and_clears_scope_once_empty()
    {
        var service = NewService();
        var context = TestServerCallContext.Create("user-1");
        await service.AddItem(new AddItemRequest
        {
            StudioId = "studio-1",
            IssuerScope = "STUDIO",
            Item = new CartItem { ProductVersionId = "product-1", Quantity = 1 },
        }, context);

        var cart = await service.RemoveItem(new RemoveItemRequest { ProductVersionId = "product-1" }, context);

        Assert.Empty(cart.Items);
        Assert.Equal("", cart.StudioId);
        Assert.Equal("", cart.IssuerScope);

        // A cleared cart accepts a new, different issuer scope without conflict.
        var next = await service.AddItem(new AddItemRequest
        {
            StudioId = "studio-2",
            IssuerScope = "PLATFORM",
            Item = new CartItem { ProductVersionId = "product-2", Quantity = 1 },
        }, context);
        Assert.Equal("studio-2", next.StudioId);
    }

    [Fact]
    public async Task EmptyCart_clears_all_items()
    {
        var service = NewService();
        var context = TestServerCallContext.Create("user-1");
        await service.AddItem(new AddItemRequest
        {
            StudioId = "studio-1",
            IssuerScope = "STUDIO",
            Item = new CartItem { ProductVersionId = "product-1", Quantity = 3 },
        }, context);

        await service.EmptyCart(new EmptyCartRequest(), context);

        var cart = await service.GetCart(new GetCartRequest(), context);
        Assert.Empty(cart.Items);
    }

    [Fact]
    public async Task Concurrent_additions_are_both_kept()
    {
        // Two requests read the same (empty) cart. The second one commits while the first is
        // between its read and its write; the first must retry on the fresh value instead of
        // overwriting the second's line.
        var store = new InMemoryCartStore();
        var service = NewService(store);
        var context = TestServerCallContext.Create("user-1");
        var interleaved = false;
        store.BeforeCompareAndSet = async _ =>
        {
            if (interleaved) return;
            interleaved = true;
            store.BeforeCompareAndSet = null;
            await service.AddItem(Add("product-1"), context);
        };

        var cart = await service.AddItem(Add("product-1"), context);

        Assert.Equal(2, Assert.Single(cart.Items).Quantity);
        Assert.Equal(1, store.RejectedWrites);
        Assert.Equal(3, store.CompareAndSetCalls);
        var reloaded = await service.GetCart(new GetCartRequest(), context);
        Assert.Equal(2, Assert.Single(reloaded.Items).Quantity);
    }

    [Fact]
    public async Task Concurrent_additions_of_different_products_keep_every_line()
    {
        var store = new InMemoryCartStore();
        var service = NewService(store);
        var context = TestServerCallContext.Create("user-1");
        var interleaved = false;
        store.BeforeCompareAndSet = async _ =>
        {
            if (interleaved) return;
            interleaved = true;
            store.BeforeCompareAndSet = null;
            await service.AddItem(Add("product-2"), context);
        };

        await service.AddItem(Add("product-1"), context);

        var cart = await service.GetCart(new GetCartRequest(), context);
        Assert.Equal(
            new[] { "product-1", "product-2" },
            cart.Items.Select(item => item.ProductVersionId).OrderBy(id => id));
    }

    [Fact]
    public async Task A_cart_that_keeps_changing_is_reported_as_aborted()
    {
        var store = new InMemoryCartStore();
        var service = NewService(store);
        var context = TestServerCallContext.Create("user-1");
        var competitor = NewService(store);
        store.BeforeCompareAndSet = async _ =>
        {
            // Every attempt loses to a competing write; the request must give up with a
            // retryable status rather than spin forever or overwrite the competitor.
            var hook = store.BeforeCompareAndSet;
            store.BeforeCompareAndSet = null;
            await competitor.AddItem(Add("product-2"), context);
            store.BeforeCompareAndSet = hook;
        };

        var ex = await Assert.ThrowsAsync<RpcException>(() => service.AddItem(Add("product-1"), context));

        Assert.Equal(StatusCode.Aborted, ex.StatusCode);
        Assert.Equal(CartGrpcService.MaxUpdateAttempts, store.RejectedWrites);
        var cart = await service.GetCart(new GetCartRequest(), context);
        var line = Assert.Single(cart.Items);
        Assert.Equal("product-2", line.ProductVersionId);
        Assert.Equal(CartGrpcService.MaxUpdateAttempts, line.Quantity);
    }

    [Fact]
    public async Task Carts_are_isolated_per_user()
    {
        var service = NewService();
        await service.AddItem(new AddItemRequest
        {
            StudioId = "studio-1",
            IssuerScope = "STUDIO",
            Item = new CartItem { ProductVersionId = "product-1", Quantity = 1 },
        }, TestServerCallContext.Create("user-1"));

        var otherUsersCart = await service.GetCart(new GetCartRequest(), TestServerCallContext.Create("user-2"));

        Assert.Empty(otherUsersCart.Items);
    }
}
