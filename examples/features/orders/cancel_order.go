package orders

import (
	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/data"
	"github.com/carlosxfelipe/gota/examples/domain"
)

// CancelOrderRequest defines the path parameter for cancellation.
type CancelOrderRequest struct {
	OrderId string `param:"orderId" doc:"ID do pedido a cancelar" example:"b1a2c3d4-0001-4000-8000-000000000001" validate:"required"`
}

// CancelOrderResponse defines the result of cancelling an order.
type CancelOrderResponse struct {
	Message string `json:"message" doc:"Mensagem de status" example:"Pedido cancelado com sucesso."`
}

// CancelOrderEndpoint handles order cancellation.
type CancelOrderEndpoint struct {
	Store *data.Store
}

// Handle cancels an order unless it was already shipped.
func (e *CancelOrderEndpoint) Handle(ctx *gota.Context, req CancelOrderRequest) (CancelOrderResponse, error) {
	order, ok := e.Store.GetOrder(req.OrderId)
	if !ok {
		return CancelOrderResponse{}, gota.NotFound("Pedido não encontrado")
	}

	if order.Status == domain.StatusShipped {
		return CancelOrderResponse{}, gota.BadRequest("Pedidos já enviados não podem ser cancelados.")
	}

	order.Status = domain.StatusCancelled
	e.Store.SaveOrder(order)

	return CancelOrderResponse{
		Message: "Pedido cancelado com sucesso.",
	}, nil
}
