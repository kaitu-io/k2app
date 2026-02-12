//go:build !(darwin || (linux && !android))

package directdial

func (d *Dialer) control() controlFunc {
	return nil // No socket binding; rely on route exclusion.
}
