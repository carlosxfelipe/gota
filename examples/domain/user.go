package domain

// User represents an authenticated user in the system.
type User struct {
	ID       int    `json:"id" doc:"User ID" example:"1"`
	Username string `json:"username" doc:"Username" example:"carlos"`
	Password string `json:"-"`
	Role     string `json:"role" doc:"User role (Admin or Customer)" example:"Customer"`
}
