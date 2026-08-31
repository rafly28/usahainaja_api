package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"usahainaja/backend/internal/app"
)

func (a *API) listBusinessMembers(w http.ResponseWriter, r *http.Request) {
	items, err := a.service.ListBusinessMembers(r.Context(), businessFrom(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) inviteBusinessMember(w http.ResponseWriter, r *http.Request) {
	var input app.InviteBusinessMemberInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.service.InviteBusinessMember(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), input)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}

func (a *API) updateBusinessMember(w http.ResponseWriter, r *http.Request) {
	var input app.UpdateBusinessMemberInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.service.UpdateBusinessMember(r.Context(), sessionFrom(r.Context()), businessFrom(r.Context()), chi.URLParam(r, "userCode"), input)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}
