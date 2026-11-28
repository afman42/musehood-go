package musehood

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// --- Tool todo (fitur omp 11): checklist per ruang ---
//
// room.Todos menyimpan item checklist (pending/active/done/blocked).
// Tool todo_add/todo_update/todo_list dipakai kursi; panel UI ruang
// menampilkan dan mengubahnya lewat state (roomViewsLocked.todos).

const maxRoomTodos = 100

var todoStatuses = map[string]bool{"pending": true, "active": true, "done": true, "blocked": true}

func roomTodosView(r room) []map[string]any {
	out := make([]map[string]any, 0, len(r.Todos))
	for _, todo := range r.Todos {
		out = append(out, map[string]any{
			"id": todo.ID, "text": todo.Text, "status": todo.Status,
			"createdAt": todo.CreatedAt, "updatedAt": todo.UpdatedAt,
		})
	}
	return out
}

func formatTodos(r *room) string {
	if len(r.Todos) == 0 {
		return "Checklist ruang kosong."
	}
	var out strings.Builder
	for i, todo := range r.Todos {
		mark := map[string]string{"pending": "[ ]", "active": "[~]", "done": "[x]", "blocked": "[!]"}[todo.Status]
		out.WriteString(fmt.Sprintf("%d. %s %s (id %s)\n", i+1, mark, todo.Text, todo.ID))
	}
	return strings.TrimSpace(out.String())
}

func (a *App) toolTodoAdd(ctx context.Context, arg string) string {
	text := strings.TrimSpace(arg)
	if text == "" {
		return "Error: format todo_add — teks item checklist."
	}
	if len(text) > 500 {
		text = text[:500]
	}
	info := toolRunInfoFrom(ctx)
	if info.RoomID == "" {
		return "Error: todo_add hanya bisa dipakai di dalam ruang."
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.data.findRoom(info.RoomID)
	if r == nil {
		return "Error: ruang tidak ditemukan."
	}
	if len(r.Todos) >= maxRoomTodos {
		return fmt.Sprintf("Error: checklist ruang penuh (maks %d item).", maxRoomTodos)
	}
	todo := roomTodo{ID: uuid(), Text: text, Status: "pending", CreatedAt: now()}
	r.Todos = append(r.Todos, todo)
	if err := a.saveLocked(); err != nil {
		return "Error: gagal menyimpan todo."
	}
	return fmt.Sprintf("Todo ditambahkan (id %s): %s", todo.ID, text)
}

func (a *App) toolTodoUpdate(ctx context.Context, arg string) string {
	lines := strings.Split(arg, "\n")
	if len(lines) < 2 {
		return "Error: format todo_update — baris pertama id/nomor item, baris kedua status (pending/active/done/blocked)."
	}
	ref := strings.TrimSpace(lines[0])
	status := strings.ToLower(strings.TrimSpace(lines[1]))
	if !todoStatuses[status] {
		return "Error: status harus pending, active, done, atau blocked."
	}
	info := toolRunInfoFrom(ctx)
	if info.RoomID == "" {
		return "Error: todo_update hanya bisa dipakai di dalam ruang."
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.data.findRoom(info.RoomID)
	if r == nil {
		return "Error: ruang tidak ditemukan."
	}
	index := -1
	if n, err := strconv.Atoi(ref); err == nil && n >= 1 && n <= len(r.Todos) {
		index = n - 1
	} else {
		for i, todo := range r.Todos {
			if todo.ID == ref {
				index = i
				break
			}
		}
	}
	if index < 0 {
		return fmt.Sprintf("Error: item todo %q tidak ditemukan.", ref)
	}
	r.Todos[index].Status = status
	r.Todos[index].UpdatedAt = now()
	if err := a.saveLocked(); err != nil {
		return "Error: gagal menyimpan todo."
	}
	return fmt.Sprintf("Todo %q sekarang %s.", r.Todos[index].Text, status)
}

func (a *App) toolTodoList(ctx context.Context) string {
	info := toolRunInfoFrom(ctx)
	if info.RoomID == "" {
		return "Error: todo_list hanya bisa dipakai di dalam ruang."
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.data.findRoom(info.RoomID)
	if r == nil {
		return "Error: ruang tidak ditemukan."
	}
	return formatTodos(r)
}

// addRoomTodoLocked menambah todo lewat API pemilik (panel UI).
func (a *App) addRoomTodoLocked(r *room, text string) (roomTodo, error) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 500 {
		return roomTodo{}, problem(400, "Teks todo wajib 1-500 karakter.")
	}
	if len(r.Todos) >= maxRoomTodos {
		return roomTodo{}, problem(400, "Checklist ruang penuh.")
	}
	todo := roomTodo{ID: uuid(), Text: text, Status: "pending", CreatedAt: now()}
	r.Todos = append(r.Todos, todo)
	return todo, nil
}

// updateRoomTodoLocked mengubah status todo lewat API pemilik.
func (a *App) updateRoomTodoLocked(r *room, id, status string) error {
	if !todoStatuses[status] {
		return problem(400, "Status todo tidak valid.")
	}
	for i := range r.Todos {
		if r.Todos[i].ID == id {
			r.Todos[i].Status = status
			r.Todos[i].UpdatedAt = now()
			return nil
		}
	}
	return problem(404, "Todo tidak ditemukan.")
}

// deleteRoomTodoLocked menghapus todo lewat API pemilik.
func (a *App) deleteRoomTodoLocked(r *room, id string) error {
	for i := range r.Todos {
		if r.Todos[i].ID == id {
			r.Todos = append(r.Todos[:i], r.Todos[i+1:]...)
			return nil
		}
	}
	return problem(404, "Todo tidak ditemukan.")
}
