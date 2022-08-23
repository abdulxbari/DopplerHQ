/*
Copyright © 2022 Doppler <support@doppler.com>
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
package utils

import (
	"regexp"
	"strings"
)

func IsDotNETSettingsFile(fileName string) bool {
	matched, _ := regexp.MatchString(`(?i)appsettings(\.[a-z0-9_-]+)?\.json`, fileName)
	return matched
}

func IsJavaSpringPropertiesFile(fileName string) bool {
	return strings.HasSuffix(fileName, "application.properties")
}
