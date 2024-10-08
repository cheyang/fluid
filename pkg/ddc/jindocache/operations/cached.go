/*
Copyright 2023 The Fluid Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package operations

import (
	"time"

	"github.com/fluid-cloudnative/fluid/pkg/utils"
)

// clean cache with a preset timeout of 60s
func (a JindoFileUtils) CleanCache() (err error) {
	var (
		// jindo jfs -formatCache -force
		command = []string{"jindocache", "-formatCache"}
		stdout  string
		stderr  string
	)

	stdout, stderr, err = a.exec(command, false)

	if err != nil {
		if utils.IgnoreNotFound(err) == nil {
			a.log.Info("Ignoring not found error when cleaning cache, maybe engine is already teared down", "error", err)
			return nil
		}
		a.log.Error(err, "JindoFileUtils.CleanCache() failed", "stdout", stdout, "stderr", stderr)
		return
	} else {
		time.Sleep(30 * time.Second)
	}

	return
}
