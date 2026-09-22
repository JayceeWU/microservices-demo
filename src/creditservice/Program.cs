using Microsoft.AspNetCore.Server.Kestrel.Core;
using Npgsql;
using OpenTelemetry.Metrics;
using OpenTelemetry.Resources;
using OpenTelemetry.Trace;
using BayAreaDanceHub.Credit.Application;
using BayAreaDanceHub.Credit.Domain;
using BayAreaDanceHub.Credit.Infrastructure;

if (args is ["--healthcheck"])
{
    using var healthClient = new HttpClient { Timeout = TimeSpan.FromSeconds(2) };
    using var response = await healthClient.GetAsync("http://127.0.0.1:8080/healthz");
    Environment.ExitCode = response.IsSuccessStatusCode ? 0 : 1;
    return;
}
var builder = WebApplication.CreateBuilder(args);
var connectionString = builder.Configuration["DATABASE_URL"]
    ?? throw new InvalidOperationException("DATABASE_URL is required");
var serviceNamespace = builder.Configuration["OTEL_SERVICE_NAMESPACE"]
    ?? throw new InvalidOperationException("OTEL_SERVICE_NAMESPACE is required");
var deploymentEnvironment = builder.Configuration["ENVIRONMENT"]
    ?? throw new InvalidOperationException("ENVIRONMENT is required");

builder.WebHost.ConfigureKestrel(options =>
{
    options.ListenAnyIP(8080, listen => listen.Protocols = HttpProtocols.Http1);
    options.ListenAnyIP(9090, listen => listen.Protocols = HttpProtocols.Http2);
});
builder.Services.AddSingleton(NpgsqlDataSource.Create(connectionString));
builder.Services.AddSingleton<ICreditStore, PostgresCreditStore>();
builder.Services.AddSingleton<CreditApplicationService>();
var meshPolicy = MeshPeerPolicy.FromEnvironment(name => builder.Configuration[name]);
builder.Services.AddSingleton(meshPolicy);
builder.Services.AddGrpc(options =>
{
    options.EnableDetailedErrors = builder.Environment.IsDevelopment();
    options.Interceptors.Add<MeshPeerInterceptor>();
});
builder.Services
    .AddGrpcHealthChecks(options => options.Services.Map("", _ => true))
    .AddCheck("self", () => Microsoft.Extensions.Diagnostics.HealthChecks.HealthCheckResult.Healthy());
builder.Services.AddOpenTelemetry()
    // Same resource identity as the Go, Node.js and Python services, so one trace/metric
    // query (service.namespace + deployment.environment.name) covers every language.
    .ConfigureResource(resource => resource
        .AddService("creditservice", serviceNamespace: serviceNamespace)
        .AddAttributes(new Dictionary<string, object> { ["deployment.environment.name"] = deploymentEnvironment }))
    .WithTracing(tracing => tracing
        .AddAspNetCoreInstrumentation()
        .AddSource("Npgsql")
        .AddOtlpExporter())
    .WithMetrics(metrics => metrics
        .AddAspNetCoreInstrumentation()
        .AddRuntimeInstrumentation()
        .AddOtlpExporter());

var app = builder.Build();
app.Logger.LogInformation("mesh peer enforcement enabled={Enabled} trustDomain={TrustDomain}", meshPolicy.Enforce, meshPolicy.TrustDomain);
app.MapGrpcService<CreditGrpcService>();
app.MapGrpcHealthChecksService();
app.MapGet("/healthz", async (NpgsqlDataSource db, CancellationToken cancellationToken) =>
{
    try
    {
        await using var command = db.CreateCommand("SELECT 1");
        await command.ExecuteScalarAsync(cancellationToken);
        return Results.Ok(new { status = "ok", service = "creditservice" });
    }
    catch
    {
        return Results.Problem("Database is unavailable", statusCode: 503);
    }
});
app.Run();
