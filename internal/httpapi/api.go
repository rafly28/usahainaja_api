package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"usahainaja/backend/internal/app"
)

const maxJSONBody = 1 << 20

type API struct {
	service      *app.Service
	cookieName   string
	cookieSecure bool
}

func New(service *app.Service, cookieName string, cookieSecure bool) http.Handler {
	api := &API{service: service, cookieName: cookieName, cookieSecure: cookieSecure}
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(requestLogger)
	router.Use(middleware.Recoverer)
	router.Use(securityHeaders)
	router.NotFound(api.notFound)
	router.MethodNotAllowed(api.methodNotAllowed)

	router.Route("/api", func(r chi.Router) {
		r.Get("/health", api.health)
		r.Route("/auth", func(r chi.Router) {
			r.Post("/register", api.register)
			r.Post("/login", api.login)
			r.Group(func(r chi.Router) {
				r.Use(api.requireSession)
				r.Get("/csrf", api.csrf)
				r.Get("/me", api.me)
				r.With(api.requireCSRF).Post("/logout", api.logout)
				r.With(api.requireCSRF).Post("/switch-business", api.switchBusiness)
			})
		})
		r.Group(func(r chi.Router) {
			r.Use(api.requireSession)
			r.Get("/businesses", api.listBusinesses)
			r.Get("/businesses/", api.listBusinesses)
			r.With(api.requireCSRF).Post("/businesses", api.createBusiness)
			r.With(api.requireCSRF).Post("/businesses/", api.createBusiness)
			r.With(api.requireBusiness).Get("/businesses/current", api.currentBusiness)
			r.With(api.requireBusiness, api.requireCSRF, api.requireRole("OWNER", "ADMIN")).Put("/businesses/current/configuration", api.updateBusinessConfiguration)
			r.With(api.requireBusiness, api.requireModule(app.ModuleCatalog)).Get("/categories", api.listCategories)
			r.With(api.requireBusiness, api.requireModule(app.ModuleCatalog), api.requireCSRF, api.requireRole("OWNER", "ADMIN")).Post("/categories", api.createCategory)
			r.With(api.requireBusiness, api.requireModule(app.ModuleCatalog), api.requireCSRF, api.requireRole("OWNER", "ADMIN")).Patch("/categories/{code}", api.updateCategory)
			r.With(api.requireBusiness, api.requireModule(app.ModuleCatalog)).Get("/units", api.listUnits)
			r.With(api.requireBusiness, api.requireModule(app.ModuleCatalog), api.requireCSRF, api.requireRole("OWNER", "ADMIN")).Post("/units", api.createUnit)
			r.With(api.requireBusiness, api.requireModule(app.ModuleCatalog), api.requireCSRF, api.requireRole("OWNER", "ADMIN")).Patch("/units/{code}", api.updateUnit)
			r.With(api.requireBusiness).Get("/locations", api.listLocations)
			r.With(api.requireBusiness, api.requireCSRF, api.requireRole("OWNER", "ADMIN")).Post("/locations", api.createLocation)
			r.With(api.requireBusiness, api.requireCSRF, api.requireRole("OWNER", "ADMIN")).Patch("/locations/{code}", api.updateLocation)
			r.With(api.requireBusiness).Get("/parties", api.listParties)
			r.With(api.requireBusiness, api.requireCSRF, api.requireRole("OWNER", "ADMIN")).Post("/parties", api.createParty)
			r.With(api.requireBusiness, api.requireCSRF, api.requireRole("OWNER", "ADMIN")).Patch("/parties/{code}", api.updateParty)
		})
		r.Group(func(r chi.Router) {
			r.Use(api.requireSession)
			r.Use(api.requireBusiness)
			r.With(api.requireModule(app.ModuleCatalog)).Get("/products", api.listProducts)
			r.With(api.requireModule(app.ModuleCatalog)).Get("/products/", api.listProducts)
			r.With(api.requireModule(app.ModuleCatalog), api.requireCSRF, api.requireRole("OWNER", "ADMIN")).Post("/products", api.createProduct)
			r.With(api.requireModule(app.ModuleCatalog), api.requireCSRF, api.requireRole("OWNER", "ADMIN")).Post("/products/", api.createProduct)
			r.With(api.requireModule(app.ModuleCatalog), api.requireCSRF, api.requireRole("OWNER", "ADMIN")).Patch("/products/{code}", api.updateProduct)
			r.With(api.requireModule(app.ModuleCatalog), api.requireCSRF, api.requireRole("OWNER", "ADMIN")).Delete("/products/{code}", api.deleteProduct)
			r.Get("/contacts", api.listContacts)
			r.With(api.requireCSRF).Post("/contacts", api.createContact)

			r.With(api.requireModule(app.ModuleFinance)).Get("/cash-accounts", api.listCashAccounts)
			r.With(api.requireModule(app.ModuleFinance), api.requireCSRF).Post("/cash-accounts", api.createCashAccount)

			r.With(api.requireModule(app.ModuleSales)).Get("/sales", api.listSales)
			r.With(api.requireModule(app.ModuleSales), api.requireCSRF).Post("/sales", api.createSale)

			r.With(api.requireModule(app.ModulePurchase)).Get("/purchases", api.listPurchases)
			r.With(api.requireModule(app.ModulePurchase), api.requireCSRF).Post("/purchases", api.createPurchase)
			r.With(api.requireModule(app.ModulePurchase), api.requireCSRF).Post("/purchases/{number}/receive", api.receivePurchase)
			r.With(api.requireModule(app.ModulePurchase), api.requireCSRF).Post("/purchases/{number}/payments", api.payPurchase)
		})
		r.Route("/inventory", func(r chi.Router) {
			r.Use(api.requireSession)
			r.Use(api.requireBusiness)
			r.With(api.requireModule(app.ModuleInventory)).Get("/products", api.listInventoryProducts)
			r.With(api.requireModule(app.ModuleInventory), api.requireCSRF, api.requireRole("OWNER", "ADMIN")).Post("/opening-stock", api.openingStock)
			r.With(api.requireModule(app.ModuleInventory)).Get("/movements", api.listStockMovements)
			r.With(api.requireModule(app.ModuleInventory), api.requireCSRF, api.requireRole("OWNER", "ADMIN")).Post("/adjustments", api.createStockAdjustment)
			r.With(api.requireModule(app.ModuleInventory), api.requireCSRF, api.requireRole("OWNER", "ADMIN")).Post("/adjustments/{number}/complete", api.completeStockAdjustment)
		})
	})
	return router
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.service.Ping(ctx); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "Database belum siap.", nil)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "ok"})
}

type registerRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var request registerRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, credentials, err := a.service.Register(r.Context(), app.RegisterInput{
		Name: request.Name, Email: request.Email, Password: request.Password,
	}, sessionMeta(r), a.rawCookie(r))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	a.setSessionCookie(w, credentials)
	w.Header().Set("Cache-Control", "no-store")
	writeData(w, http.StatusCreated, result)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, credentials, err := a.service.Login(r.Context(), app.LoginInput{
		Email: request.Email, Password: request.Password,
	}, sessionMeta(r), a.rawCookie(r))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	a.setSessionCookie(w, credentials)
	w.Header().Set("Cache-Control", "no-store")
	writeData(w, http.StatusOK, result)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if err := a.service.Logout(r.Context(), sessionFrom(r.Context()).ID); err != nil {
		writeAppError(w, r, err)
		return
	}
	a.clearSessionCookie(w)
	w.Header().Set("Cache-Control", "no-store")
	writeData(w, http.StatusOK, map[string]string{"message": "Berhasil keluar."})
}

func (a *API) csrf(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeData(w, http.StatusOK, map[string]string{"csrf_token": sessionFrom(r.Context()).CSRFToken})
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	session := sessionFrom(r.Context())
	var active *app.Business
	if session.ActiveBusinessID != nil {
		business, err := a.service.BusinessContext(r.Context(), session)
		if err == nil {
			value := business.Business
			active = &value
		} else {
			var appErr *app.Error
			if !errors.As(err, &appErr) || appErr.Code == "INTERNAL_ERROR" {
				writeAppError(w, r, err)
				return
			}
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeData(w, http.StatusOK, map[string]any{"user": session.User, "active_business": active})
}

type switchBusinessRequest struct {
	BusinessCode string `json:"business_code"`
}

func (a *API) switchBusiness(w http.ResponseWriter, r *http.Request) {
	var request switchBusinessRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	business, err := a.service.SwitchBusiness(r.Context(), sessionFrom(r.Context()), request.BusinessCode)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"active_business": business.Business})
}

func (a *API) listBusinesses(w http.ResponseWriter, r *http.Request) {
	items, err := a.service.ListBusinesses(r.Context(), sessionFrom(r.Context()).UserID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"items": items})
}

type createBusinessRequest struct {
	Name         string `json:"name"`
	BusinessType string `json:"business_type"`
	Timezone     string `json:"timezone"`
	Currency     string `json:"currency"`
}

func (a *API) createBusiness(w http.ResponseWriter, r *http.Request) {
	var request createBusinessRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	business, err := a.service.CreateBusiness(r.Context(), sessionFrom(r.Context()), app.CreateBusinessInput{
		Name: request.Name, BusinessType: request.BusinessType, Timezone: request.Timezone, Currency: request.Currency,
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, business.Business)
}

func (a *API) currentBusiness(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, businessFrom(r.Context()).Business)
}

type updateBusinessConfigurationRequest struct {
	BusinessType   string   `json:"business_type"`
	EnabledModules []string `json:"enabled_modules"`
}

func (a *API) updateBusinessConfiguration(w http.ResponseWriter, r *http.Request) {
	var request updateBusinessConfigurationRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	business, err := a.service.UpdateBusinessConfiguration(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), app.UpdateBusinessConfigurationInput{BusinessType: request.BusinessType, EnabledModules: request.EnabledModules})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, business.Business)
}

type decimalJSON string

func (d *decimalJSON) UnmarshalJSON(value []byte) error {
	if string(value) == "null" {
		*d = ""
		return nil
	}
	if len(value) > 0 && value[0] == '"' {
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return err
		}
		*d = decimalJSON(text)
		return nil
	}
	*d = decimalJSON(string(value))
	return nil
}

type createProductRequest struct {
	Name                 string      `json:"name"`
	SKU                  string      `json:"sku"`
	Barcode              string      `json:"barcode"`
	BaseUnitSymbol       string      `json:"base_unit_symbol"`
	DefaultPurchasePrice decimalJSON `json:"default_purchase_price"`
	DefaultSellingPrice  decimalJSON `json:"default_selling_price"`
	MinStock             decimalJSON `json:"min_stock"`
	IsStockTracked       *bool       `json:"is_stock_tracked"`
}

func (a *API) listProducts(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	items, err := a.service.ListProducts(r.Context(), businessFrom(r.Context()).ID, search)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) createProduct(w http.ResponseWriter, r *http.Request) {
	var request createProductRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	product, err := a.service.CreateProduct(r.Context(), businessFrom(r.Context()).ID, app.CreateProductInput{
		Name: request.Name, SKU: request.SKU, Barcode: request.Barcode, BaseUnitSymbol: request.BaseUnitSymbol,
		DefaultPurchasePrice: string(request.DefaultPurchasePrice),
		DefaultSellingPrice:  string(request.DefaultSellingPrice), MinStock: string(request.MinStock),
		IsStockTracked: request.IsStockTracked,
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, product)
}

func (a *API) updateProduct(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	var request createProductRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	product, err := a.service.UpdateProduct(r.Context(), businessFrom(r.Context()).ID, code, app.CreateProductInput{
		Name: request.Name, SKU: request.SKU, Barcode: request.Barcode, BaseUnitSymbol: request.BaseUnitSymbol,
		DefaultPurchasePrice: string(request.DefaultPurchasePrice),
		DefaultSellingPrice:  string(request.DefaultSellingPrice), MinStock: string(request.MinStock),
		IsStockTracked: request.IsStockTracked,
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, product)
}

func (a *API) deleteProduct(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	err := a.service.DeleteProduct(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()).ID, code)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type openingStockRequest struct {
	ProductCode  string      `json:"product_code"`
	LocationCode string      `json:"location_code"`
	Quantity     decimalJSON `json:"quantity"`
	Reason       string      `json:"reason"`
}

func (a *API) openingStock(w http.ResponseWriter, r *http.Request) {
	var request openingStockRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := a.service.CreateOpeningStock(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()).ID, app.OpeningStockInput{
		ProductCode: request.ProductCode, LocationCode: request.LocationCode,
		Quantity: string(request.Quantity), Reason: request.Reason,
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}

func (a *API) listInventoryProducts(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	items, err := a.service.ListInventoryProducts(r.Context(), businessFrom(r.Context()).ID, search)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) listStockMovements(w http.ResponseWriter, r *http.Request) {
	movements, err := a.service.ListStockMovements(r.Context(), businessFrom(r.Context()).ID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"movements": movements})
}

func (a *API) createStockAdjustment(w http.ResponseWriter, r *http.Request) {
	var in app.NewStockAdjustment
	if !decodeJSON(w, r, &in) {
		return
	}
	result, err := a.service.CreateStockAdjustment(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()).ID, in)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}

func (a *API) completeStockAdjustment(w http.ResponseWriter, r *http.Request) {
	number := chi.URLParam(r, "number")
	err := a.service.CompleteStockAdjustment(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()).ID, number)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "COMPLETED"})
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		slog.Debug("HTTP Request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration", time.Since(start).String(),
			"ip", r.RemoteAddr,
		)
	})
}

func (a *API) notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusNotFound, "ROUTE_NOT_FOUND", "Endpoint tidak ditemukan.", nil)
}

func (a *API) methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Metode HTTP tidak didukung.", nil)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, r, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type harus application/json.", nil)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "Body JSON tidak valid.", nil)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "Body hanya boleh berisi satu objek JSON.", nil)
		return false
	}
	return true
}

func sessionMeta(r *http.Request) app.SessionMeta {
	ip := r.RemoteAddr
	if forwarded := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(forwarded) != nil {
		ip = forwarded
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		if net.ParseIP(ip) == nil {
			ip = host
		}
	}
	userAgent := r.UserAgent()
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	return app.SessionMeta{UserAgent: userAgent, IPAddress: ip}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func (a *API) rawCookie(r *http.Request) string {
	cookie, err := r.Cookie(a.cookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (a *API) setSessionCookie(w http.ResponseWriter, credentials app.SessionCredentials) {
	maxAge := int(time.Until(credentials.ExpiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{
		Name: a.cookieName, Value: credentials.Token, Path: "/", HttpOnly: true,
		Secure: a.cookieSecure, SameSite: http.SameSiteLaxMode,
		Expires: credentials.ExpiresAt, MaxAge: maxAge,
	})
}

func (a *API) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: a.cookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: a.cookieSecure, SameSite: http.SameSiteLaxMode,
		Expires: time.Unix(1, 0), MaxAge: -1,
	})
}

func normalizedHeader(value string) string { return strings.TrimSpace(value) }
