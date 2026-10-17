package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"usahainaja/backend/internal/app"
)

// New menyusun router dan seluruh endpoint. Definisi rute dipisah dari handler
// agar mudah dibaca dan tetap di bawah batas ukuran file.
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
		api.routeAuth(r)
		api.routeMasterData(r)
		api.routeTransactions(r)
		api.routeInventory(r)
	})
	return router
}

func (a *API) routeAuth(r chi.Router) {
	r.Route("/auth", func(r chi.Router) {
		r.Post("/register", a.register)
		r.Post("/login", a.login)
		r.Group(func(r chi.Router) {
			r.Use(a.requireSession)
			r.Get("/csrf", a.csrf)
			r.Get("/me", a.me)
			r.With(a.requireCSRF).Post("/logout", a.logout)
			r.With(a.requireCSRF).Post("/switch-business", a.switchBusiness)
		})
	})
}

func (a *API) routeMasterData(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(a.requireSession)
		r.Get("/businesses", a.listBusinesses)
		r.Get("/businesses/", a.listBusinesses)
		r.With(a.requireCSRF).Post("/businesses", a.createBusiness)
		r.With(a.requireCSRF).Post("/businesses/", a.createBusiness)
		r.With(a.requireBusiness).Get("/businesses/current", a.currentBusiness)
		r.With(a.requireBusiness, a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Put("/businesses/current/configuration", a.updateBusinessConfiguration)
		r.With(a.requireBusiness, a.requireRole("OWNER", "ADMIN")).Get("/businesses/current/members", a.listBusinessMembers)
		r.With(a.requireBusiness, a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Post("/businesses/current/members", a.inviteBusinessMember)
		r.With(a.requireBusiness, a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Patch("/businesses/current/members/{userCode}", a.updateBusinessMember)
		r.With(a.requireBusiness, a.requireModule(app.ModuleCatalog)).Get("/categories", a.listCategories)
		r.With(a.requireBusiness, a.requireModule(app.ModuleCatalog), a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Post("/categories", a.createCategory)
		r.With(a.requireBusiness, a.requireModule(app.ModuleCatalog), a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Patch("/categories/{code}", a.updateCategory)
		r.With(a.requireBusiness, a.requireModule(app.ModuleCatalog)).Get("/units", a.listUnits)
		r.With(a.requireBusiness, a.requireModule(app.ModuleCatalog), a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Post("/units", a.createUnit)
		r.With(a.requireBusiness, a.requireModule(app.ModuleCatalog), a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Patch("/units/{code}", a.updateUnit)
		r.With(a.requireBusiness, a.requireModule(app.ModuleCatalog)).Get("/unit-conversions", a.listUnitConversions)
		r.With(a.requireBusiness, a.requireModule(app.ModuleCatalog), a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Post("/unit-conversions", a.createUnitConversion)
		r.With(a.requireBusiness).Get("/locations", a.listLocations)
		r.With(a.requireBusiness, a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Post("/locations", a.createLocation)
		r.With(a.requireBusiness, a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Patch("/locations/{code}", a.updateLocation)
		r.With(a.requireBusiness).Get("/parties", a.listParties)
		r.With(a.requireBusiness, a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Post("/parties", a.createParty)
		r.With(a.requireBusiness, a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Patch("/parties/{code}", a.updateParty)
	})
}

func (a *API) routeTransactions(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(a.requireSession)
		r.Use(a.requireBusiness)
		r.With(a.requireModule(app.ModuleCatalog)).Get("/products", a.listProducts)
		r.With(a.requireModule(app.ModuleCatalog)).Get("/products/", a.listProducts)
		r.With(a.requireModule(app.ModuleCatalog), a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Post("/products", a.createProduct)
		r.With(a.requireModule(app.ModuleCatalog), a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Post("/products/", a.createProduct)
		r.With(a.requireModule(app.ModuleCatalog), a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Patch("/products/{code}", a.updateProduct)
		r.With(a.requireModule(app.ModuleCatalog), a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Delete("/products/{code}", a.deleteProduct)
		r.Get("/contacts", a.listContacts)
		r.With(a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Post("/contacts", a.createContact)

		r.With(a.requireModule(app.ModuleFinance)).Get("/cash-accounts", a.listCashAccounts)
		r.With(a.requireModule(app.ModuleFinance), a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Post("/cash-accounts", a.createCashAccount)

		a.routeSales(r)
		a.routePurchases(r)
	})
}

func (a *API) routeSales(r chi.Router) {
	r.With(a.requireModule(app.ModuleSales)).Get("/sales", a.listSales)
	r.With(a.requireModule(app.ModuleSales), a.requireCSRF, a.requireRole("OWNER", "ADMIN", "CASHIER"), a.requireIdempotency("SALE_CREATE")).Post("/sales", a.createSale)
	r.With(a.requireModule(app.ModuleSales)).Get("/sales/{number}", a.getSale)
	r.With(a.requireModule(app.ModuleSales)).Get("/sales/{number}/receipt", a.saleReceipt)
	r.With(a.requireModule(app.ModuleSales), a.requireCSRF, a.requireRole("OWNER", "ADMIN", "CASHIER"), a.requireIdempotency("SALE_ITEM_ADD")).Post("/sales/{number}/items", a.addSaleItem)
	r.With(a.requireModule(app.ModuleSales), a.requireCSRF, a.requireRole("OWNER", "ADMIN", "CASHIER"), a.requireIdempotency("SALE_ITEM_UPDATE")).Put("/sales/{number}/items/{line}", a.updateSaleItem)
	r.With(a.requireModule(app.ModuleSales), a.requireCSRF, a.requireRole("OWNER", "ADMIN", "CASHIER"), a.requireIdempotency("SALE_ITEM_DELETE")).Delete("/sales/{number}/items/{line}", a.deleteSaleItem)
	r.With(a.requireModule(app.ModuleSales), a.requireCSRF, a.requireRole("OWNER", "ADMIN"), a.requireIdempotency("SALE_CANCEL")).Post("/sales/{number}/cancel", a.cancelSale)
	r.With(a.requireModule(app.ModuleSales), a.requireCSRF, a.requireRole("OWNER", "ADMIN", "CASHIER"), a.requireIdempotency("SALE_CHECKOUT")).Post("/sales/{number}/checkout", a.checkoutSale)
	r.With(a.requireModule(app.ModuleSales), a.requireCSRF, a.requireRole("OWNER", "ADMIN"), a.requireIdempotency("SALE_VOID")).Post("/sales/{number}/void", a.voidSale)
}

func (a *API) routePurchases(r chi.Router) {
	r.With(a.requireModule(app.ModulePurchase)).Get("/purchases", a.listPurchases)
	r.With(a.requireModule(app.ModulePurchase), a.requireCSRF, a.requireRole("OWNER", "ADMIN"), a.requireIdempotency("PURCHASE_CREATE")).Post("/purchases", a.createPurchase)
	r.With(a.requireModule(app.ModulePurchase)).Get("/purchases/{number}", a.getPurchase)
	r.With(a.requireModule(app.ModulePurchase)).Get("/purchases/{number}/receipt", a.purchaseReceipt)
	r.With(a.requireModule(app.ModulePurchase), a.requireCSRF, a.requireRole("OWNER", "ADMIN"), a.requireIdempotency("PURCHASE_ITEM_ADD")).Post("/purchases/{number}/items", a.addPurchaseItem)
	r.With(a.requireModule(app.ModulePurchase), a.requireCSRF, a.requireRole("OWNER", "ADMIN"), a.requireIdempotency("PURCHASE_ITEM_UPDATE")).Put("/purchases/{number}/items/{line}", a.updatePurchaseItem)
	r.With(a.requireModule(app.ModulePurchase), a.requireCSRF, a.requireRole("OWNER", "ADMIN"), a.requireIdempotency("PURCHASE_ITEM_DELETE")).Delete("/purchases/{number}/items/{line}", a.deletePurchaseItem)
	r.With(a.requireModule(app.ModulePurchase), a.requireCSRF, a.requireRole("OWNER", "ADMIN"), a.requireIdempotency("PURCHASE_ORDER")).Post("/purchases/{number}/order", a.orderPurchase)
	r.With(a.requireModule(app.ModulePurchase), a.requireCSRF, a.requireRole("OWNER", "ADMIN"), a.requireIdempotency("PURCHASE_RECEIVE")).Post("/purchases/{number}/receive", a.receivePurchase)
	r.With(a.requireModule(app.ModulePurchase), a.requireCSRF, a.requireRole("OWNER", "ADMIN"), a.requireIdempotency("PURCHASE_PAY")).Post("/purchases/{number}/payments", a.payPurchase)
	r.With(a.requireModule(app.ModulePurchase), a.requireCSRF, a.requireRole("OWNER", "ADMIN"), a.requireIdempotency("PURCHASE_CANCEL")).Post("/purchases/{number}/cancel", a.cancelPurchase)
}

func (a *API) routeInventory(r chi.Router) {
	r.Route("/inventory", func(r chi.Router) {
		r.Use(a.requireSession)
		r.Use(a.requireBusiness)
		r.With(a.requireModule(app.ModuleInventory)).Get("/products", a.listInventoryProducts)
		r.With(a.requireModule(app.ModuleInventory), a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Post("/opening-stock", a.openingStock)
		r.With(a.requireModule(app.ModuleInventory)).Get("/movements", a.listStockMovements)
		r.With(a.requireModule(app.ModuleInventory)).Get("/adjustments", a.listStockAdjustments)
		r.With(a.requireModule(app.ModuleInventory)).Get("/adjustments/{number}", a.getStockAdjustment)
		r.With(a.requireModule(app.ModuleInventory), a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Post("/adjustments", a.createStockAdjustment)
		r.With(a.requireModule(app.ModuleInventory), a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Post("/adjustments/{number}/complete", a.completeStockAdjustment)
		r.With(a.requireModule(app.ModuleInventory)).Get("/opnames", a.listStockOpnames)
		r.With(a.requireModule(app.ModuleInventory)).Get("/opnames/{number}", a.getStockOpname)
		r.With(a.requireModule(app.ModuleInventory), a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Post("/opnames", a.createStockOpname)
		r.With(a.requireModule(app.ModuleInventory), a.requireCSRF, a.requireRole("OWNER", "ADMIN")).Post("/opnames/{number}/complete", a.completeStockOpname)
	})
}
