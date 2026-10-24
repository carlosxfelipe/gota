package orders

import (
	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/data"
	"github.com/carlosxfelipe/gota/examples/domain"
)

// PayOrderRequest defines the path parameter for payment.
type PayOrderRequest struct {
	OrderId string `param:"orderId" doc:"ID do pedido a ser pago" example:"b1a2c3d4-0001-4000-8000-000000000001" validate:"required"`
}

// PayOrderResponse defines the result of paying an order.
type PayOrderResponse struct {
	Message string `json:"message" doc:"Mensagem de status" example:"Pedido pago com sucesso! Refrescando com uma Coca-Cola bem gelada!"`
}

// PayOrderEndpoint handles order payment.
type PayOrderEndpoint struct {
	Store *data.Store
}

// Handle transitions order to 'Paid'.
func (e *PayOrderEndpoint) Handle(ctx *gota.Context, req PayOrderRequest) (PayOrderResponse, error) {
	order, ok := e.Store.GetOrder(req.OrderId)
	if !ok {
		return PayOrderResponse{}, gota.NotFound("Pedido não encontrado")
	}

	if order.Status != domain.StatusCreated {
		return PayOrderResponse{}, gota.BadRequest("Apenas pedidos no status inicial podem ser pagos.")
	}

	if len(order.Items) == 0 {
		return PayOrderResponse{}, gota.BadRequest("Não é possível pagar um pedido sem itens.")
	}

	order.Status = domain.StatusPaid
	e.Store.SaveOrder(order)

	return PayOrderResponse{
		Message: "Pedido pago com sucesso! Refrescando com uma Coca-Cola bem gelada!",
	}, nil
}
