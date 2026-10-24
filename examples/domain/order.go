package domain

import (
	"crypto/rand"
	"fmt"
)

// OrderStatus defines the lifecycle status of an order.
type OrderStatus string

const (
	StatusCreated   OrderStatus = "Created"
	StatusPaid      OrderStatus = "Paid"
	StatusShipped   OrderStatus = "Shipped"
	StatusCancelled OrderStatus = "Cancelled"
)

// OrderItem represents a line item within an order.
type OrderItem struct {
	ID          string  `json:"id" doc:"Identificador único do item" example:"item_1"`
	ProductName string  `json:"productName" doc:"Nome do produto" example:"Coca-Cola Lata 350ml"`
	Price       float64 `json:"price" doc:"Preço unitário do produto" example:"5.50"`
	Quantity    int     `json:"quantity" doc:"Quantidade adquirida" example:"2"`
}

// Order represents an order in the system.
type Order struct {
	ID           string      `json:"id" doc:"Identificador único do pedido" example:"b1a2c3d4-0001-4000-8000-000000000001"`
	UserID       int         `json:"userId" doc:"ID do usuário proprietário do pedido" example:"2"`
	CustomerName string      `json:"customerName" doc:"Nome do cliente" example:"Carlos Felipe"`
	Status       OrderStatus `json:"status" doc:"Estado atual do pedido" example:"Created"`
	Items        []OrderItem `json:"items" doc:"Lista de itens do pedido"`
}

// TotalAmount calculates the sum of price * quantity for all items in the order.
func (o *Order) TotalAmount() float64 {
	var total float64
	for _, it := range o.Items {
		total += it.Price * float64(it.Quantity)
	}
	return total
}

// NewUUID generates a valid v4 UUID string using crypto/rand.
func NewUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
