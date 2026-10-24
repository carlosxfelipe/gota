package orders

import (
	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/data"
	"github.com/carlosxfelipe/gota/examples/domain"
)

// GetOrderRequest defines the path parameter to retrieve an order.
type GetOrderRequest struct {
	OrderId string `param:"orderId" doc:"ID único do pedido" example:"b1a2c3d4-0001-4000-8000-000000000001"`
	ID      string `param:"id" doc:"ID único do pedido"`
}

// GetOrderResponse returns complete order details.
type GetOrderResponse struct {
	Id           string             `json:"id" doc:"ID do pedido" example:"b1a2c3d4-0001-4000-8000-000000000001"`
	CustomerName string             `json:"customerName" doc:"Nome do cliente" example:"Carlos Felipe"`
	Status       string             `json:"status" doc:"Status do pedido" example:"Created"`
	TotalAmount  float64            `json:"totalAmount" doc:"Valor total acumulado dos itens" example:"36.00"`
	Items        []domain.OrderItem `json:"items" doc:"Lista de itens do pedido"`
	Message      string             `json:"message" doc:"Mensagem de status" example:"Pedido recuperado com sucesso!"`
}

// GetOrderEndpoint handles order retrieval.
type GetOrderEndpoint struct {
	Store *data.Store
}

// Handle retrieves the order by ID.
func (e *GetOrderEndpoint) Handle(ctx *gota.Context, req GetOrderRequest) (GetOrderResponse, error) {
	id := req.OrderId
	if id == "" {
		id = req.ID
	}
	order, ok := e.Store.GetOrder(id)
	if !ok {
		return GetOrderResponse{}, gota.NotFound("Pedido não encontrado")
	}

	return GetOrderResponse{
		Id:           order.ID,
		CustomerName: order.CustomerName,
		Status:       string(order.Status),
		TotalAmount:  order.TotalAmount(),
		Items:        order.Items,
		Message:      "Pedido recuperado com sucesso!",
	}, nil
}
