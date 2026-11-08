package repositories

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// eventsChannel carries everything the dashboard shows live. Each payload is
// {"event": name, "data": {...}}, whether Go code or a trigger sends it.
const eventsChannel = "tasklane_events"

type Events struct {
	db *pgxpool.Pool
}

func NewEvents(db *pgxpool.Pool) *Events {
	return &Events{db: db}
}

// Notify sends payload to every process listening for events.
func (r *Events) Notify(ctx context.Context, payload string) error {
	_, err := r.db.Exec(ctx, `select pg_notify($1, $2)`, eventsChannel, payload)
	return err
}

// Listen calls handle with every event payload until ctx ends or the connection
// fails. listening runs once the subscription is in place.
func (r *Events) Listen(ctx context.Context, listening func(), handle func(payload string)) error {
	return listen(ctx, r.db, eventsChannel, listening, handle)
}
