package main

import (
	"io"

	"github.com/atlassian/git-lob/providers/smart"
)

func queryCaps(req *smart.JsonRequest, in io.Reader, out io.Writer, config *Config, path string) *smart.JsonResponse {

	// This server always supports binary deltas
	// Send/receive settings may cause actual requests to be rejected
	caps := []string{"binary_delta"}

	result := smart.QueryCapsResponse{Caps: caps}
	resp, err := smart.NewJsonResponse(req.Id, result)
	if err != nil {
		resp = smart.NewJsonErrorResponse(req.Id, err.Error())
	}

	return resp
}

func setCaps(req *smart.JsonRequest, in io.Reader, out io.Writer, config *Config, path string) *smart.JsonResponse {
	// Actually not required in this reference implementation yet
	result := smart.SetEnabledCapsResponse{}
	resp, err := smart.NewJsonResponse(req.Id, result)
	if err != nil {
		resp = smart.NewJsonErrorResponse(req.Id, err.Error())
	}

	return resp
}
