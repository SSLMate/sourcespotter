// Copyright (C) 2025 Opsmate, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a
// copy of this software and associated documentation files (the "Software"),
// to deal in the Software without restriction, including without limitation
// the rights to use, copy, modify, merge, publish, distribute, sublicense,
// and/or sell copies of the Software, and to permit persons to whom the
// Software is furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included
// in all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL
// THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR
// OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE,
// ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
// OTHER DEALINGS IN THE SOFTWARE.
//
// Except as contained in this notice, the name(s) of the above copyright
// holders shall not be used in advertising or otherwise to promote the
// sale, use or other dealings in this Software without prior written
// authorization.

package toolchain

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"software.sslmate.com/src/sourcespotter"
)

// GarbageCollectArtifacts deletes objects from the bucket's out/ folder that do
// not correspond to a row in the toolchain_build table. If dryrun is true, the
// objects are not actually deleted.
func GarbageCollectArtifacts(ctx context.Context, dryrun bool) error {
	client := newS3Client()
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(Bucket),
		Prefix: aws.String("out/"),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("error listing bucket: %w", err)
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			version, buildID, ok := parseArtifactKey(key)
			if !ok {
				log.Printf("deleting %s because object name is invalid", key)
				if !dryrun {
					if err := deleteObject(ctx, key); err != nil {
						return fmt.Errorf("error deleting %s: %w", key, err)
					}
				}
				continue
			}
			var exists bool
			err := sourcespotter.DB.QueryRowContext(ctx,
				`SELECT EXISTS (SELECT 1 FROM toolchain_build WHERE version = $1 AND build_id = $2)`,
				version, buildID).Scan(&exists)
			if err != nil {
				return fmt.Errorf("error querying toolchain_build: %w", err)
			}
			if !exists {
				log.Printf("deleting %s because there is no matching toolchain_build row", key)
				if !dryrun {
					if err := deleteObject(ctx, key); err != nil {
						return fmt.Errorf("error deleting %s: %w", key, err)
					}
				}
				continue
			}
		}
	}
	return nil
}

func parseArtifactKey(key string) (version string, buildID []byte, ok bool) {
	if !strings.HasPrefix(key, "out/") {
		return "", nil, false
	}
	rest := strings.TrimPrefix(key, "out/")
	last := strings.LastIndexByte(rest, '.')
	if last == -1 {
		return "", nil, false
	}
	beforeExt := rest[:last]
	secondLast := strings.LastIndexByte(beforeExt, '.')
	if secondLast == -1 {
		return "", nil, false
	}
	version = beforeExt[:secondLast]
	hexid := beforeExt[secondLast+1:]
	id, err := hex.DecodeString(hexid)
	if err != nil || len(id) == 0 {
		return "", nil, false
	}
	return version, id, true
}
