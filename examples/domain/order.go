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
	ID          string  `json:"id" doc:"Unique item identifier" example:"item_1"`
	ProductName string  `json:"productName" doc:"Product name" example:"Coca-Cola Can 350ml"`
	Price       float64 `json:"price" doc:"Unit price" example:"5.50"`
	Quantity    int     `json:"quantity" doc:"Quantity purchased" example:"2"`
}

// ShippingAddress represents the delivery address for an order with rich validation rules.
type ShippingAddress struct {
	Street  string `json:"street" doc:"Street address" example:"Av. Paulista, 1000" validate:"required,min=3" msg:"required:Street address is required|min:Street address must be at least 3 characters"`
	City    string `json:"city" doc:"City" example:"São Paulo" validate:"required" msg:"City is required"`
	ZipCode string `json:"zipCode" doc:"8-digit postal code (CEP)" example:"01310100" validate:"required,len=8" msg:"required:Postal code is required|len:Postal code must be exactly 8 digits"`
}

// Order represents an order in the system.
type Order struct {
	ID              string           `json:"id" doc:"Unique order identifier" example:"b1a2c3d4-0001-4000-8000-000000000001"`
	UserID          int              `json:"userId" doc:"ID of the user who owns the order" example:"2"`
	CustomerName    string           `json:"customerName" doc:"Customer name" example:"Carlos Felipe"`
	Status          OrderStatus      `json:"status" doc:"Current status of the order" example:"Created"`
	Items           []OrderItem      `json:"items" doc:"List of order items"`
	ShippingAddress *ShippingAddress `json:"shippingAddress,omitempty" doc:"Shipping address with nested validation"`
	ReceiptFilename string           `json:"receiptFilename,omitempty" doc:"Attached receipt filename" example:"receipt.pdf"`
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
