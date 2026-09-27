// Package v1 is the route table of the HTTP API's first version: every path,
// its method and its handler, declared in one file.
//
// The table is read as source as well as compiled. The interface's tests find
// each registration by its shape - app.GET(base+"/path", handler) - and hold the
// set against the OpenAPI document that is served, so a route registered in any
// other shape is one that comparison cannot see.
package v1
