package domain

// User represents an authenticated user in the system.
type User struct {
	ID       int    `json:"id" doc:"ID do usuário" example:"1"`
	Username string `json:"username" doc:"Nome de usuário" example:"carlos"`
	Password string `json:"-"`
	Role     string `json:"role" doc:"Perfil de acesso (Admin ou Customer)" example:"Customer"`
}
