package repo

// UpdateRequest holds info about an extension which needs to be updated.
// This is kept for potential future use with a worker queue pattern.
type UpdateRequest struct {
	Slug string
	Repo RepoType
}
