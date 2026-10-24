package orders

import (
	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/data"
	"github.com/carlosxfelipe/gota/examples/domain"
)

// PayOrderRequest defines the path parameter for payment.
type PayOrderRequest struct {
	OrderId string `param:"orderId" doc:"Order ID to be paid" example:"b1a2c3d4-0001-4000-8000-000000000001" validate:"required"`
}

// PayOrderResponse defines the result of paying an order.
type PayOrderResponse struct {
	Message string `json:"message" doc:"Status message" example:"Order paid successfully! Enjoy an ice-cold Coca-Cola!"`
}

// PayOrderEndpoint handles order payment.
type PayOrderEndpoint struct {
	Store *data.Store
}

// Handle transitions order to 'Paid'.
func (e *PayOrderEndpoint) Handle(ctx *gota.Context, req PayOrderRequest) (PayOrderResponse, error) {
	order, ok := e.Store.GetOrder(req.OrderId)
	if !ok {
		return PayOrderResponse{}, gota.NotFound("Order not found")
	}

	if order.Status != domain.StatusCreated {
		return PayOrderResponse{}, gota.BadRequest("Only orders in initial state can be paid.")
	}

	if len(order.Items) == 0 {
		return PayOrderResponse{}, gota.BadRequest("Cannot pay an order with no items.")
	}

	order.Status = domain.StatusPaid
	e.Store.SaveOrder(order)

	return PayOrderResponse{
		Message: "Order paid successfully! Enjoy an ice-cold Coca-Cola!",
	}, nil
}
