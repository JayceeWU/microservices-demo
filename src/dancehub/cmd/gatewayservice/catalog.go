package main

import (
	"net/http"
	"time"

	catalogv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/catalog/v1"
	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
)

func (g *gateway) catalog(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := grpcRequestContext(r, 3*time.Second)
	defer cancel()
	page := &commonv1.PageRequest{PageSize: queryPageSize(r)}
	switch r.Pattern {
	case "GET /v1/studios":
		response, err := g.catalogClient.SearchStudios(ctx, &catalogv1.SearchStudiosRequest{Query: r.URL.Query().Get("query"), Page: page})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		items := make([]map[string]any, 0, len(response.Studios))
		for _, item := range response.Studios {
			items = append(items, studioJSON(item))
		}
		platform.JSON(w, http.StatusOK, map[string]any{"studios": items, "nextPageToken": response.GetPage().GetNextPageToken()})
	case "GET /v1/studios/{id}":
		item, err := g.catalogClient.GetStudio(ctx, &catalogv1.GetStudioRequest{Id: r.PathValue("id")})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusOK, studioJSON(item))
	case "GET /v1/studios/{studioId}/rooms":
		response, err := g.catalogClient.ListRooms(ctx, &catalogv1.ListRoomsRequest{StudioId: r.PathValue("studioId"), Page: page})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		items := make([]map[string]any, 0, len(response.Rooms))
		for _, item := range response.Rooms {
			items = append(items, roomJSON(item))
		}
		platform.JSON(w, http.StatusOK, map[string]any{"rooms": items, "nextPageToken": response.GetPage().GetNextPageToken()})
	case "GET /v1/credit-products":
		response, err := g.catalogClient.SearchCreditProducts(ctx, &catalogv1.SearchCreditProductsRequest{StudioId: r.URL.Query().Get("studio_id"), IncludePlatform: true, Page: page})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		items := make([]map[string]any, 0, len(response.Products))
		for _, item := range response.Products {
			items = append(items, productJSON(item))
		}
		platform.JSON(w, http.StatusOK, map[string]any{"products": items, "nextPageToken": response.GetPage().GetNextPageToken()})
	case "GET /v1/credit-products/{id}":
		item, err := g.catalogClient.GetCreditProductVersion(ctx, &catalogv1.GetCreditProductVersionRequest{Id: r.PathValue("id")})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusOK, productJSON(item))
	}
}
