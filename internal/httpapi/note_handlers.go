package httpapi

import "net/http"

// note_handlers.go: 便笺 CRUD（账本共享，成员均可维护；权限由 mustMembership 校验）。

func (s *server) listNotes(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	notes, err := s.ledger.ListNotes(m.ledgerID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": notes})
}

func (s *server) createNote(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	n, err := s.ledger.CreateNote(m.ledgerID, sessionOf(r).UserID, body.Content)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

func (s *server) updateNote(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		Content string `json:"content"`
		Pinned  bool   `json:"pinned"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := s.ledger.UpdateNote(m.ledgerID, r.PathValue("id"), body.Content, body.Pinned); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) deleteNote(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	if err := s.ledger.DeleteNote(m.ledgerID, r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
