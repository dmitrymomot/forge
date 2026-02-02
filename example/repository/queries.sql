-- name: ListContacts :many
SELECT id, name, email, created_at
FROM contacts
ORDER BY created_at DESC;

-- name: GetContact :one
SELECT id, name, email, created_at
FROM contacts
WHERE id = $1;

-- name: CreateContact :one
INSERT INTO contacts (name, email)
VALUES ($1, $2)
RETURNING id, name, email, created_at;

-- name: DeleteContact :exec
DELETE FROM contacts
WHERE id = $1;
