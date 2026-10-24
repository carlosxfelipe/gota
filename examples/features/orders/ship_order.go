package orders

import (
	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/data"
	"github.com/carlosxfelipe/gota/examples/domain"
)

// ShipOrderRequest defines the path parameter for shipping.
type ShipOrderRequest struct {
	OrderId string `param:"orderId" doc:"ID do pedido a enviar" example:"b1a2c3d4-0002-4000-8000-000000000002" validate:"required"`
}

// ShipOrderResponse defines the result of shipping an order.
type ShipOrderResponse struct {
	Message string `json:"message" doc:"Mensagem de status" example:"Pedido enviado! Suas Coca-Colas estão a caminho!"`
}

// ShipOrderEndpoint handles order shipping.
type ShipOrderEndpoint struct {
	Store *data.Store
}

// Handle transitions order to 'Shipped'.
func (e *ShipOrderEndpoint) Handle(ctx *gota.Context, req ShipOrderRequest) (ShipOrderResponse, error) {
	order, ok := e.Store.GetOrder(req.OrderId)
	if !ok {
		return ShipOrderResponse{}, gota.NotFound("Pedido não encontrado")
	}

	if order.Status != domain.StatusPaid {
		return ShipOrderResponse{}, gota.BadRequest("Apenas pedidos pagos podem ser enviados.")
	}

	order.Status = domain.StatusShipped
	e.Store.SaveOrder(order)

	return ShipOrderResponse{
		Message: "Pedido enviado! Suas Coca-Colas estão a caminho!",
	}, nil
}
