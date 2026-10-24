package orders

import (
	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/data"
	"github.com/carlosxfelipe/gota/examples/domain"
	"github.com/carlosxfelipe/gota/examples/features/auth"
)

// CreateOrderRequest defines the input payload to create a new order.
type CreateOrderRequest struct {
	CustomerName string `json:"customerName" doc:"Nome do cliente" example:"Carlos Felipe" validate:"required,min=3"`
}

// CreateOrderResponse defines the result of creating a new order.
type CreateOrderResponse struct {
	OrderId string `json:"orderId" doc:"ID do pedido criado" example:"b1a2c3d4-0001-4000-8000-000000000001"`
	Message string `json:"message" doc:"Mensagem de confirmação" example:"Pedido criado com sucesso!"`
}

// CreateOrderEndpoint handles order creation (matches FastEndpoints CreateOrderEndpoint).
type CreateOrderEndpoint struct {
	Store *data.Store
}

// Handle creates the order in initial 'Created' state.
func (e *CreateOrderEndpoint) Handle(ctx *gota.Context, req CreateOrderRequest) (CreateOrderResponse, error) {
	userID := 0
	if user, ok := auth.CurrentUser(ctx); ok {
		userID = user.UserID
	}

	order := domain.Order{
		ID:           domain.NewUUID(),
		UserID:       userID,
		CustomerName: req.CustomerName,
		Status:       domain.StatusCreated,
		Items:        []domain.OrderItem{},
	}

	e.Store.SaveOrder(order)

	return CreateOrderResponse{
		OrderId: order.ID,
		Message: "Pedido criado com sucesso!",
	}, nil
}
