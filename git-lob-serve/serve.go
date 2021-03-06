package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/atlassian/git-lob/providers/smart"
	"github.com/atlassian/git-lob/util"
)

type MethodFunc func(req *smart.JsonRequest, in io.Reader, out io.Writer, config *Config, path string) *smart.JsonResponse

var methodMap = map[string]MethodFunc{
	"QueryCaps":            queryCaps,
	"SetEnabledCaps":       setCaps,
	"FileExists":           fileExists,
	"FileExistsOfSize":     fileExistsOfSize,
	"LOBExists":            lobExists,
	"UploadFile":           uploadFile,
	"DownloadFilePrepare":  downloadFilePrepare,
	"DownloadFileStart":    downloadFileStart,
	"PickCompleteLOB":      pickCompleteLOB,
	"UploadDelta":          uploadDelta,
	"DownloadDeltaPrepare": downloadDeltaPrepare,
	"DownloadDeltaStart":   downloadDeltaStart,
}

// these methods can't return any error responses
var bytestreamResponseMethods = util.NewStringSetFromSlice([]string{
	"DownloadFileStart",
	"DownloadDeltaStart",
})

func Serve(in io.Reader, out io.Writer, outerr io.Writer, config *Config, path string) int {

	// Read input from client on stdin, buffered so we can detect terminators for JSON

	rdr := bufio.NewReader(in)
	// we keep reading until stdin is closed
	for {
		jsonbytes, err := rdr.ReadBytes(byte(0))
		if err != nil {
			if err == io.EOF {
				// normal exit
				break
			}
			fmt.Fprintf(outerr, "Unable to read from client: %v\n", err.Error())
			return 21
		}
		// slice off the terminator
		jsonbytes = jsonbytes[:len(jsonbytes)-1]
		var req smart.JsonRequest
		err = json.Unmarshal(jsonbytes, &req)
		if err != nil {
			fmt.Fprintf(outerr, "Unable to unmarhsal JSON: %v: %v\n", string(jsonbytes), err.Error())
			return 22
		}

		// Special case 'Exit'
		if req.Method == "Exit" {
			result := &smart.ExitResponse{}
			resp, _ := smart.NewJsonResponse(req.Id, result)
			sendResponse(resp, out)
			return 0
		}

		// Get function to handle method
		f, ok := methodMap[req.Method]
		var resp *smart.JsonResponse
		if !ok {
			// Since it was valid JSON otherwise, send error as response
			resp = smart.NewJsonErrorResponse(req.Id, fmt.Sprintf("Unknown method %v", req.Method))
		} else {
			// method found, process
			resp = f(&req, rdr, out, config, path)
		}
		// There may not have been a JSON response; that might be because method just streams bytes
		// in which case we just ignore this bit
		if resp != nil {
			if resp.Error != "" && bytestreamResponseMethods.Contains(req.Method) {
				// there was an error but this was a bytestream-only method so can't return JSON
				// just send it to stderr
				fmt.Fprintf(outerr, "%v\n", resp.Error)
				return 33
			} else {
				// normal method which responds in JSON
				err := sendResponse(resp, out)
				if err != nil {
					fmt.Fprintf(outerr, "%v\n", err.Error())
					return 23
				}
			}
		}

		// Ready for next request from client

	}

	return 0
}

func sendResponse(resp *smart.JsonResponse, out io.Writer) error {
	responseBytes, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("Unable to marhsal JSON response: %v: %v", resp, err.Error())
	}
	// null terminate response
	responseBytes = append(responseBytes, byte(0))
	_, err = out.Write(responseBytes)
	return err
}
