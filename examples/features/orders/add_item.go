package orders

import (
	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/data"
	"github.com/carlosxfelipe/gota/examples/domain"
)

// AddItemRequest defines the route parameters and body to add an item to an order.
type AddItemRequest struct {
	OrderId     string  `param:"orderId" doc:"ID do pedido" example:"b1a2c3d4-0001-4000-8000-000000000001" validate:"required"`
	ProductName string  `json:"productName" doc:"Nome do item" example:"Coca-Cola Lata 350ml" validate:"required"`
	Price       float64 `json:"price" doc:"Preço unitário" example:"5.50" validate:"required,min=0.01"`
	Quantity    int     `json:"quantity" doc:"Quantidade" example:"2" validate:"required,min=1"`
}

// AddItemResponse defines the result of adding an item.
type AddItemResponse struct {
	Message string `json:"message" doc:"Mensagem de status" example:"Item adicionado ao pedido com sucesso!"`
	ItemId  string `json:"itemId" doc:"ID do item adicionado" example:"item_1"`
}

// AddItemEndpoint handles adding items to an open order.
type AddItemEndpoint struct {
	Store *data.Store
}

// Handle adds an item if the order is still in 'Created' state.
func (e *AddItemEndpoint) Handle(ctx *gota.Context, req AddItemRequest) (AddItemResponse, error) {
	order, ok := e.Store.GetOrder(req.OrderId)
	if !ok {
		return AddItemResponse{}, gota.NotFound("Pedido não encontrado")
	}

	if order.Status != domain.StatusCreated {
		return AddItemResponse{}, gota.BadRequest("Não é possível adicionar itens. O pedido não está no estado inicial.")
	}

	item := domain.OrderItem{
		ID:          domain.NewUUID(),
		ProductName: req.ProductName,
		Price:       req.Price,
		Quantity:    req.Quantity,
	}

	order.Items = append(order.Items, item)
	e.Store.SaveOrder(order)

	return AddItemResponse{
		Message: "Item adicionado ao pedido com sucesso!",
		ItemId:  item.ID,
	}, nil
}
