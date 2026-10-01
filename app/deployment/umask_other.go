//go:build !linux

package deployment

// ACL protection on other platforms must be established by the deployment operator.
func RestrictCreation() {}
