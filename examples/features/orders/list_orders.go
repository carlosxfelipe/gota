package orders

import (
	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/data"
	"github.com/carlosxfelipe/gota/examples/features/auth"
)

// OrderSummary provides a high-level summary of an order in the listing.
type OrderSummary struct {
	Id           string  `json:"id" doc:"ID do pedido" example:"b1a2c3d4-0001-4000-8000-000000000001"`
	CustomerName string  `json:"customerName" doc:"Nome do cliente" example:"Carlos Felipe"`
	Status       string  `json:"status" doc:"Status do pedido" example:"Created"`
	TotalAmount  float64 `json:"totalAmount" doc:"Valor total do pedido" example:"36.00"`
}

// ListOrdersResponse returns a list of order summaries.
type ListOrdersResponse struct {
	Orders []OrderSummary `json:"orders" doc:"Lista de pedidos"`
}

// ListOrdersEndpoint lists orders.
type ListOrdersEndpoint struct {
	Store *data.Store
}

// Handle lists orders with role-based scoping (Customer sees own, Admin sees all).
func (e *ListOrdersEndpoint) Handle(ctx *gota.Context, _ gota.Empty) (ListOrdersResponse, error) {
	userID := 0
	isAdmin := false

	if user, ok := auth.CurrentUser(ctx); ok {
		userID = user.UserID
		isAdmin = user.Role == "Admin"
	}

	domainOrders := e.Store.ListOrders(userID, isAdmin)

	summaries := make([]OrderSummary, 0, len(domainOrders))
	for _, o := range domainOrders {
		summaries = append(summaries, OrderSummary{
			Id:           o.ID,
			CustomerName: o.CustomerName,
			Status:       string(o.Status),
			TotalAmount:  o.TotalAmount(),
		})
	}

	return ListOrdersResponse{
		Orders: summaries,
	}, nil
}
