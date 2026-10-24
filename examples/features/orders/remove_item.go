package orders

import (
	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/data"
	"github.com/carlosxfelipe/gota/examples/domain"
)

// RemoveItemRequest defines the path parameters to delete an item.
type RemoveItemRequest struct {
	OrderId string `param:"orderId" doc:"ID do pedido" example:"b1a2c3d4-0001-4000-8000-000000000001" validate:"required"`
	ItemId  string `param:"itemId" doc:"ID do item a remover" example:"item_101" validate:"required"`
}

// RemoveItemResponse defines the result of removing an item.
type RemoveItemResponse struct {
	Message string `json:"message" doc:"Mensagem de status" example:"Item removido com sucesso!"`
}

// RemoveItemEndpoint handles item removal.
type RemoveItemEndpoint struct {
	Store *data.Store
}

// Handle removes an item from an open order.
func (e *RemoveItemEndpoint) Handle(ctx *gota.Context, req RemoveItemRequest) (RemoveItemResponse, error) {
	order, ok := e.Store.GetOrder(req.OrderId)
	if !ok {
		return RemoveItemResponse{}, gota.NotFound("Pedido não encontrado")
	}

	if order.Status != domain.StatusCreated {
		return RemoveItemResponse{}, gota.BadRequest("Não é possível remover itens. O pedido não está mais em estado de criação.")
	}

	foundIdx := -1
	for idx, it := range order.Items {
		if it.ID == req.ItemId {
			foundIdx = idx
			break
		}
	}

	if foundIdx == -1 {
		return RemoveItemResponse{}, gota.NotFound("Item não encontrado no pedido")
	}

	order.Items = append(order.Items[:foundIdx], order.Items[foundIdx+1:]...)
	e.Store.SaveOrder(order)

	return RemoveItemResponse{
		Message: "Item removido com sucesso!",
	}, nil
}
