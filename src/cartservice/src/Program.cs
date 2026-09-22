using Microsoft.AspNetCore.Server.Kestrel.Core;
using OpenTelemetry.Metrics;
using OpenTelemetry.Resources;
using OpenTelemetry.Trace;
using StackExchange.Redis;

if (args is ["--healthcheck"])
{
    using var healthClient = new HttpClient { Timeout = TimeSpan.FromSeconds(2) };
    using var response = await healthClient.GetAsync("http://127.0.0.1:8080/healthz");
    Environment.ExitCode = response.IsSuccessStatusCode ? 0 : 1;
    return;
}

var builder=WebApplication.CreateBuilder(args);
builder.WebHost.ConfigureKestrel(options=>{
    options.ListenAnyIP(8080,listen=>listen.Protocols=HttpProtocols.Http1);
    options.ListenAnyIP(9090,listen=>listen.Protocols=HttpProtocols.Http2);
});
var redisAddress = Environment.GetEnvironmentVariable("REDIS_ADDR")
    ?? throw new InvalidOperationException("REDIS_ADDR is required");
var serviceNamespace = Environment.GetEnvironmentVariable("OTEL_SERVICE_NAMESPACE")
    ?? throw new InvalidOperationException("OTEL_SERVICE_NAMESPACE is required");
var deploymentEnvironment = Environment.GetEnvironmentVariable("ENVIRONMENT")
    ?? throw new InvalidOperationException("ENVIRONMENT is required");
// Connect lazily and keep retrying in the background so the pod does not crash-loop while
// Redis is still starting; the readiness of individual calls is reported per request.
var redisOptions = ConfigurationOptions.Parse(redisAddress);
redisOptions.AbortOnConnectFail = false;
builder.Services.AddSingleton<IConnectionMultiplexer>(_ => ConnectionMultiplexer.Connect(redisOptions));
builder.Services.AddSingleton<ICartStore, RedisCartStore>();
var meshPolicy = MeshPeerPolicy.FromEnvironment(Environment.GetEnvironmentVariable);
builder.Services.AddSingleton(meshPolicy);
builder.Services.AddGrpc(options => options.Interceptors.Add<MeshPeerInterceptor>());
builder.Services
    .AddGrpcHealthChecks(options => options.Services.Map("", _ => true))
    .AddCheck("self", () => Microsoft.Extensions.Diagnostics.HealthChecks.HealthCheckResult.Healthy());
// Same resource identity as the Go, Node.js and Python services.
builder.Services.AddOpenTelemetry()
    .ConfigureResource(resource => resource
        .AddService("cartservice", serviceNamespace: serviceNamespace)
        .AddAttributes(new Dictionary<string, object> { ["deployment.environment.name"] = deploymentEnvironment }))
    .WithTracing(tracing => tracing.AddAspNetCoreInstrumentation().AddRedisInstrumentation().AddOtlpExporter())
    .WithMetrics(metrics => metrics.AddAspNetCoreInstrumentation().AddRuntimeInstrumentation().AddOtlpExporter());
var app=builder.Build();
app.Logger.LogInformation("mesh peer enforcement enabled={Enabled} trustDomain={TrustDomain}", meshPolicy.Enforce, meshPolicy.TrustDomain);
app.MapGrpcService<CartGrpcService>();
app.MapGrpcHealthChecksService();
// This service is published as a fully trimmed single-file binary. Returning
// plain text avoids reflection-based anonymous JSON metadata being trimmed.
app.MapGet("/healthz", () => Results.Text("ok", "text/plain"));
app.Run();
