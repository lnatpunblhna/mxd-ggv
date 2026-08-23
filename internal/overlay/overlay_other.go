//go:build !windows

package overlay

func (s *Service) wake() {}

func (s *Service) requestQuit() {}
