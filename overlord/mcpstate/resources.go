// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package mcpstate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func (p *MCPManager) listResources() map[string]any {
	descriptors := make([]ResourceDescriptor, 0, len(p.resources))
	for _, resource := range p.resources {
		descriptors = append(descriptors, resource.Descriptor())
	}
	return map[string]any{"resources": descriptors}
}

func (p *MCPManager) readResource(ctx context.Context, params json.RawMessage) (any, *responseError) {
	var req struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &responseError{Code: rpcInvalidParams, Message: "invalid resources/read params"}
	}

	if err := validateResourceReadInput(req.URI); err != nil {
		return nil, &responseError{Code: rpcInvalidParams, Message: fmt.Sprintf("invalid arguments: %v", err)}
	}

	parsedURI, err := url.Parse(req.URI)
	if err != nil {
		return nil, &responseError{Code: rpcInvalidParams, Message: fmt.Sprintf("invalid arguments: %v", err)}
	}

	routePath := "/" + parsedURI.Host + parsedURI.EscapedPath()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, routePath, nil)
	if err != nil {
		return nil, &responseError{Code: rpcInternalError, Message: err.Error()}
	}

	routeWriter := &resourceRouteWriter{}
	p.resourceRouter.ServeHTTP(routeWriter, httpReq)
	if routeWriter.resource == nil {
		return nil, &responseError{Code: rpcInvalidParams, Message: fmt.Sprintf("invalid arguments: unsupported resource uri: %s", req.URI)}
	}

	result, err := routeWriter.resource.Read(httpReq.Context(), p.st, httpReq)
	if err != nil {
		return nil, &responseError{Code: rpcInternalError, Message: err.Error()}
	}
	return result, nil
}

func validateResourceReadInput(uri string) error {
	if strings.TrimSpace(uri) == "" {
		return fmt.Errorf("uri is required")
	}

	parsedURI, err := url.Parse(uri)
	if err != nil {
		return err
	}
	if parsedURI.Scheme != "snap" {
		return fmt.Errorf("unsupported resource uri: %s", uri)
	}
	if strings.TrimSpace(parsedURI.Host) == "" {
		return fmt.Errorf("resource endpoint is required in uri")
	}
	if strings.TrimSpace(strings.Trim(parsedURI.Path, "/")) == "" {
		return fmt.Errorf("resource id is required in uri")
	}
	return nil
}

type resourceRouteHandler struct {
	resource Resource
}

func (h resourceRouteHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	rw, ok := w.(*resourceRouteWriter)
	if !ok {
		return
	}
	rw.resource = h.resource
}

type resourceRouteWriter struct {
	resource Resource
	header   http.Header
}

func (w *resourceRouteWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *resourceRouteWriter) Write(data []byte) (int, error) {
	return len(data), nil
}

func (w *resourceRouteWriter) WriteHeader(statusCode int) {
	_ = statusCode
}
