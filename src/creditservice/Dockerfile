FROM mcr.microsoft.com/dotnet/sdk:9.0-alpine AS build
WORKDIR /src
COPY creditservice.csproj .
RUN dotnet restore
COPY . .
RUN dotnet publish -c Release -o /out --no-restore

FROM mcr.microsoft.com/dotnet/aspnet:9.0-alpine
WORKDIR /app
COPY --from=build /out .
USER 1654:1654
ENV ASPNETCORE_URLS=http://+:8080
ENTRYPOINT ["dotnet", "creditservice.dll"]
