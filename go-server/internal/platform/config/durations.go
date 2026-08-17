package config

import "time"

func (s ServiceSection) StopTimeout() time.Duration {
	return time.Duration(s.StopTimeoutSeconds) * time.Second
}

func (s ServiceSection) StopInitialGrace() time.Duration {
	return time.Duration(s.StopInitialGraceSeconds) * time.Second
}

func (s ServiceSection) StopProbeInterval() time.Duration {
	return time.Duration(s.StopProbeSeconds) * time.Second
}

func (s ServiceSection) StopStableWindow() time.Duration {
	return time.Duration(s.StopStableSeconds) * time.Second
}

func (a AdminSection) ReadTimeout() time.Duration {
	return time.Duration(a.ReadTimeoutSeconds) * time.Second
}

func (a AdminSection) ReadHeaderTimeout() time.Duration {
	return time.Duration(a.ReadHeaderTimeoutSeconds) * time.Second
}

func (a AdminSection) WriteTimeout() time.Duration {
	return time.Duration(a.WriteTimeoutSeconds) * time.Second
}

func (a AdminSection) IdleTimeout() time.Duration {
	return time.Duration(a.IdleTimeoutSeconds) * time.Second
}
