package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/data"
	"github.com/carlosxfelipe/gota/examples/domain"
	"github.com/carlosxfelipe/gota/examples/features/auth"
	"github.com/carlosxfelipe/gota/examples/features/orders"
)

func setupTestApp() (*gota.App, *data.Store) {
	app := gota.New(gota.Config{
		Title:   "Order Flow API Test",
		Version: "1.0.0",
	})

	store := data.NewStore()

	// Auth (public)
	auth.RegisterPublicRoutes(app, store)

	// Protected routes (Auth + Orders)
	protectedGroup := app.Group("")
	protectedGroup.Use(gota.JWT(auth.Secret, gota.JWTOptions{
		ClaimsFactory: func() any { return &auth.UserClaims{} },
	}))
	auth.RegisterProtectedRoutes(protectedGroup, store)
	orders.RegisterRoutes(protectedGroup, store)

	return app, store
}

func doRequest(app *gota.App, method, path, token string, body any) *httptest.ResponseRecorder {
	var bodyReader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}

	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func TestOrderFlowEndToEnd(t *testing.T) {
	app, _ := setupTestApp()

	// 1. Login Carlos (Customer)
	loginRes := doRequest(app, http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": "carlos",
		"password": "carlos123",
	})
	if loginRes.Code != http.StatusOK {
		t.Fatalf("expected login 200, got %d: %s", loginRes.Code, loginRes.Body.String())
	}
	var loginBody struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(loginRes.Body.Bytes(), &loginBody); err != nil || loginBody.Token == "" {
		t.Fatalf("failed to decode login token: %v", err)
	}
	carlosToken := loginBody.Token
	t.Log("Carlos Token:", carlosToken)

	// 2. Login Admin
	adminLoginRes := doRequest(app, http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": "admin",
		"password": "admin123",
	})
	if adminLoginRes.Code != http.StatusOK {
		t.Fatalf("expected admin login 200, got %d", adminLoginRes.Code)
	}

	// 3. Unauthorized access check
	noAuthRes := doRequest(app, http.MethodGet, "/api/orders", "", nil)
	if noAuthRes.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", noAuthRes.Code)
	}

	// 4. Create Order
	createRes := doRequest(app, http.MethodPost, "/api/orders", carlosToken, map[string]string{
		"CustomerName": "Carlos",
	})
	if createRes.Code != http.StatusOK {
		t.Fatalf("expected create order 200, got %d: %s", createRes.Code, createRes.Body.String())
	}
	var createBody struct {
		OrderId string `json:"orderId"`
	}
	if err := json.Unmarshal(createRes.Body.Bytes(), &createBody); err != nil || createBody.OrderId == "" {
		t.Fatalf("failed to extract orderId: %v", err)
	}
	orderID := createBody.OrderId

	// 5. Add Item
	addItemRes := doRequest(app, http.MethodPost, "/api/orders/"+orderID+"/items", carlosToken, map[string]any{
		"ProductName": "Coca-Cola Can 350ml",
		"Price":       5.50,
		"Quantity":    2,
	})
	if addItemRes.Code != http.StatusOK {
		t.Fatalf("expected add item 200, got %d: %s", addItemRes.Code, addItemRes.Body.String())
	}
	var addItemBody struct {
		ItemId string `json:"itemId"`
	}
	if err := json.Unmarshal(addItemRes.Body.Bytes(), &addItemBody); err != nil || addItemBody.ItemId == "" {
		t.Fatalf("failed to extract itemId: %v", err)
	}
	firstItemID := addItemBody.ItemId

	// Add second item so we can remove one and still have items
	doRequest(app, http.MethodPost, "/api/orders/"+orderID+"/items", carlosToken, map[string]any{
		"ProductName": "Burger",
		"Price":       25.00,
		"Quantity":    1,
	})

	// 6. List Orders
	listRes := doRequest(app, http.MethodGet, "/api/orders", carlosToken, nil)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected list orders 200, got %d", listRes.Code)
	}

	// 7. Get Order
	getRes := doRequest(app, http.MethodGet, "/api/orders/"+orderID, carlosToken, nil)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected get order 200, got %d: %s", getRes.Code, getRes.Body.String())
	}
	var getBody struct {
		ID     string             `json:"id"`
		Status domain.OrderStatus `json:"status"`
		Items  []domain.OrderItem `json:"items"`
	}
	if err := json.Unmarshal(getRes.Body.Bytes(), &getBody); err != nil || len(getBody.Items) != 2 {
		t.Fatalf("expected 2 items in order, got %d", len(getBody.Items))
	}

	// 8. Remove Item
	removeRes := doRequest(app, http.MethodDelete, "/api/orders/"+orderID+"/items/"+firstItemID, carlosToken, nil)
	if removeRes.Code != http.StatusOK {
		t.Fatalf("expected remove item 200, got %d: %s", removeRes.Code, removeRes.Body.String())
	}

	// 9. Pay Order
	payRes := doRequest(app, http.MethodPost, "/api/orders/"+orderID+"/pay", carlosToken, nil)
	if payRes.Code != http.StatusOK {
		t.Fatalf("expected pay order 200, got %d: %s", payRes.Code, payRes.Body.String())
	}

	// 10. Ship Order
	shipRes := doRequest(app, http.MethodPost, "/api/orders/"+orderID+"/ship", carlosToken, nil)
	if shipRes.Code != http.StatusOK {
		t.Fatalf("expected ship order 200, got %d: %s", shipRes.Code, shipRes.Body.String())
	}

	// 11. Cannot cancel shipped order
	cancelShippedRes := doRequest(app, http.MethodPost, "/api/orders/"+orderID+"/cancel", carlosToken, nil)
	if cancelShippedRes.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when cancelling shipped order, got %d", cancelShippedRes.Code)
	}

	// 12. Create another order and cancel it
	cancelOrderRes := doRequest(app, http.MethodPost, "/api/orders", carlosToken, map[string]string{
		"CustomerName": "Carlos 2",
	})
	var cancelOrderBody struct {
		OrderId string `json:"orderId"`
	}
	json.Unmarshal(cancelOrderRes.Body.Bytes(), &cancelOrderBody)
	cancelSuccessRes := doRequest(app, http.MethodPost, "/api/orders/"+cancelOrderBody.OrderId+"/cancel", carlosToken, nil)
	if cancelSuccessRes.Code != http.StatusOK {
		t.Fatalf("expected 200 when cancelling open order, got %d", cancelSuccessRes.Code)
	}
}

func TestAuthLifecycle(t *testing.T) {
	app, _ := setupTestApp()

	// 1. Register new user
	regRes := doRequest(app, http.MethodPost, "/api/auth/register", "", map[string]string{
		"username": "tester",
		"password": "secretpassword",
		"role":     "Customer",
	})
	if regRes.Code != http.StatusOK {
		t.Fatalf("expected register 200, got %d: %s", regRes.Code, regRes.Body.String())
	}

	var regBody struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
		Token    string `json:"token"`
	}
	if err := json.Unmarshal(regRes.Body.Bytes(), &regBody); err != nil {
		t.Fatalf("failed to parse register response: %v", err)
	}
	if regBody.Username != "tester" || regBody.Token == "" {
		t.Fatalf("unexpected register output: %+v", regBody)
	}

	// 2. Reject duplicate username
	dupRes := doRequest(app, http.MethodPost, "/api/auth/register", "", map[string]string{
		"username": "tester",
		"password": "secretpassword2",
	})
	if dupRes.Code != http.StatusBadRequest {
		t.Fatalf("expected duplicate register 400, got %d", dupRes.Code)
	}

	// 3. Get profile (/api/auth/me) with token
	meRes := doRequest(app, http.MethodGet, "/api/auth/me", regBody.Token, nil)
	if meRes.Code != http.StatusOK {
		t.Fatalf("expected me 200, got %d: %s", meRes.Code, meRes.Body.String())
	}
	var me domain.User
	if err := json.Unmarshal(meRes.Body.Bytes(), &me); err != nil {
		t.Fatalf("failed to parse me response: %v", err)
	}
	if me.Username != "tester" || me.Role != "Customer" {
		t.Fatalf("unexpected me output: %+v", me)
	}

	// 4. Delete account (/api/auth/account)
	delRes := doRequest(app, http.MethodDelete, "/api/auth/account", regBody.Token, nil)
	if delRes.Code != http.StatusOK {
		t.Fatalf("expected delete account 200, got %d: %s", delRes.Code, delRes.Body.String())
	}

	// 5. Subsequent /api/auth/me should fail with 404
	meAfterDel := doRequest(app, http.MethodGet, "/api/auth/me", regBody.Token, nil)
	if meAfterDel.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after deletion, got %d", meAfterDel.Code)
	}

	// 6. Login with deleted user should fail
	loginRes := doRequest(app, http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": "tester",
		"password": "secretpassword",
	})
	if loginRes.Code != http.StatusBadRequest {
		t.Fatalf("expected login failure 400 after deletion, got %d", loginRes.Code)
	}
}

func TestDocsAndRedirectRoutes(t *testing.T) {
	app, _ := setupTestApp()
	app.Get("/", func(ctx *gota.Context) error {
		return ctx.Redirect("/docs")
	}).Hidden()

	// 1. Root redirect
	resRoot := doRequest(app, http.MethodGet, "/", "", nil)
	if resRoot.Code != http.StatusFound && resRoot.Code != http.StatusMovedPermanently {
		t.Fatalf("expected redirect for /, got status %d", resRoot.Code)
	}
	if resRoot.Header().Get("Location") != "/docs" {
		t.Errorf("expected Location /docs, got %s", resRoot.Header().Get("Location"))
	}

	// 2. Scalar UI HTML
	resDocs := doRequest(app, http.MethodGet, "/docs", "", nil)
	if resDocs.Code != http.StatusOK {
		t.Fatalf("expected 200 for /docs, got %d", resDocs.Code)
	}
	if !bytes.Contains(resDocs.Body.Bytes(), []byte("@scalar/api-reference")) {
		t.Errorf("expected Scalar script in /docs output")
	}

	// 3. OpenAPI 3.1 JSON spec
	resOpenAPI := doRequest(app, http.MethodGet, "/openapi.json", "", nil)
	if resOpenAPI.Code != http.StatusOK {
		t.Fatalf("expected 200 for /openapi.json, got %d", resOpenAPI.Code)
	}
	var spec map[string]any
	if err := json.Unmarshal(resOpenAPI.Body.Bytes(), &spec); err != nil {
		t.Fatalf("failed to decode openapi.json: %v", err)
	}
	if spec["openapi"] != "3.1.0" {
		t.Errorf("expected openapi 3.1.0, got %v", spec["openapi"])
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		t.Fatal("expected non-empty paths in OpenAPI spec")
	}
	expectedEndpoints := []string{
		"/api/auth/login",
		"/api/auth/register",
		"/api/auth/me",
		"/api/auth/account",
		"/api/orders",
		"/api/orders/{orderId}",
		"/api/orders/{orderId}/items",
		"/api/orders/{orderId}/items/{itemId}",
		"/api/orders/{orderId}/pay",
		"/api/orders/{orderId}/ship",
		"/api/orders/{orderId}/cancel",
	}
	for _, ep := range expectedEndpoints {
		if _, exists := paths[ep]; !exists {
			t.Errorf("expected endpoint %s in OpenAPI spec", ep)
		}
	}
}
