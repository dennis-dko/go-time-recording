// Package config exposes the application-specific settings that sit on top of
// the configuration GoFr already reads for itself (ports, database, log level,
// tracing). Only settings this application acts on belong here - or, for the
// metrics endpoint and the trace exporter, reports on: those two are GoFr's to
// act on, and are read here so the Settings screen can show what this process is
// actually doing rather than leaving it to be guessed from a file.
package config
