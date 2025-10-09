package a2asrv

// User can be attached to call context by authentication middleware.
type User interface {
	// Name returns a username.
	Name() string
	// Authenticated returns true if requested was authenticated.
	Authenticated() bool
}

// AuthenticatedUser is a simple implementation of User interface which can be configured with a username.
type AuthenticatedUser struct {
	UserName string
}

func (u *AuthenticatedUser) Name() string {
	return u.UserName
}

func (u *AuthenticatedUser) Authenticated() bool {
	return true
}

type unauthenticatedUser struct {
	UserName string
}

func (unauthenticatedUser) Name() string {
	return ""
}

func (unauthenticatedUser) Authenticated() bool {
	return false
}
