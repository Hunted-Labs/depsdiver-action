package api

import (
	"encoding/json"
	"net/http"

	"github.com/mailru/easyjson"
)

type Handler struct{}

func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	response := map[string]string{
		"status": "ok",
	}
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) GetUsers(w http.ResponseWriter, r *http.Request) {
	users := []map[string]interface{}{
		{"id": 1, "name": "Alice"},
		{"id": 2, "name": "Bob"},
	}
	json.NewEncoder(w).Encode(users)
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var user map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(user)
}

func (h *Handler) CreateUserEasyJSON(w http.ResponseWriter, r *http.Request, user easyjson.Marshaler) error {
	data, err := easyjson.Marshal(user)
	if err != nil {
		return err
	}
	w.Write(data)
	return nil
}

