/*
Copyright © 2019 Doppler <support@doppler.com>

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
package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/DopplerHQ/cli/pkg/configuration"
	"github.com/DopplerHQ/cli/pkg/http"
	"github.com/DopplerHQ/cli/pkg/models"
	"github.com/DopplerHQ/cli/pkg/utils"
	"github.com/skratchdot/open-golang/open"
	"github.com/spf13/cobra"
	"gopkg.in/gookit/color.v1"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate to Doppler",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		localConfig := configuration.LocalConfig(cmd)
		prevConfig := configuration.Get(configuration.Scope)
		yes := utils.GetBoolFlag(cmd, "yes")
		overwrite := utils.GetBoolFlag(cmd, "overwrite")
		copyAuthCode := !utils.GetBoolFlag(cmd, "no-copy")
		hostname, _ := os.Hostname()

		// Disallow overwriting a token with the same scope (by default)
		if prevConfig.Token.Value != "" && !overwrite {
			prevScope, err1 := filepath.Abs(prevConfig.Token.Scope)
			newScope, err2 := filepath.Abs(configuration.Scope)
			// specified scope already has a token
			if err1 == nil && err2 == nil && prevScope == newScope {
				cwd := utils.Cwd()
				if newScope == cwd {
					// overwriting token in CWD so show yes/no override prompt
					utils.LogWarning("This scope has already been authorized from a previous login.")
					if !utils.ConfirmationPrompt("Overwrite existing login:", false) {
						utils.Log("Exiting")
						return
					}
				} else {
					options := []string{fmt.Sprintf("Scope login to current directory (%s)", cwd), fmt.Sprintf("Overwrite existing login (%s)", newScope)}
					warning := "This scope has already been authorized from a previous login."
					message := "You may scope your new login to the current directory, or overwrite your existing login."

					// user is using default scope
					if newScope == "/" {
						warning = "You have already authorized this directory."
						message = "You may scope your new login to the current directory, or overwrite the global login."
						options[1] = fmt.Sprintf("Overwrite global login (%s)", newScope)
					}

					utils.LogWarning(warning)
					utils.Log(message)

					if utils.SelectPrompt("Select an option:", options, options[0]) == options[0] {
						// use current directory
						configuration.Scope = cwd
					}
				}
			}
		}

		response, err := http.GenerateAuthCode(localConfig.APIHost.Value, utils.GetBool(localConfig.VerifyTLS.Value, true), hostname, utils.HostOS(), utils.HostArch())
		if !err.IsNil() {
			utils.HandleError(err.Unwrap(), err.Message)
		}
		code := response["code"].(string)
		authURL := response["auth_url"].(string)

		if copyAuthCode {
			if err := utils.CopyToClipboard(code); err != nil {
				utils.LogWarning("Unable to copy to clipboard")
			}
		}

		openBrowser := yes || utils.Silent || utils.ConfirmationPrompt("Open the authorization page in your browser?", true)
		printURL := !openBrowser
		if openBrowser {
			if err := open.Run(authURL); err != nil {
				if utils.Silent {
					utils.HandleError(err, "Unable to launch a browser")
				}

				printURL = true
				utils.Log("Unable to launch a browser")
				utils.LogDebugError(err)
			}
		}

		if printURL {
			utils.Log(fmt.Sprintf("Complete authorization at %s", authURL))
		}
		utils.Log(fmt.Sprintf("Your auth code is %s", color.Green.Render(code)))
		utils.Log("Waiting...")

		// auth flow must complete within 5 minutes
		timeout := 5 * time.Minute
		completeBy := time.Now().Add(timeout)
		verifyTLS := utils.GetBool(localConfig.VerifyTLS.Value, true)

		response = nil
		for {
			// we do not respect --no-timeout here
			if time.Now().After(completeBy) {
				utils.HandleError(fmt.Errorf("login timed out after %d minutes", int(timeout.Minutes())))
			}

			resp, err := http.GetAuthToken(localConfig.APIHost.Value, verifyTLS, code)
			if !err.IsNil() {
				if err.Code == 409 {
					time.Sleep(2 * time.Second)
					continue
				}
				utils.HandleError(err.Unwrap(), err.Message)
			}

			response = resp
			break
		}

		if response == nil {
			utils.HandleError(errors.New("unexpected API response"))
		}

		if err, ok := response["error"]; ok {
			utils.Log("")
			utils.Log(fmt.Sprint(err))

			os.Exit(1)
		}

		token := response["token"].(string)
		name := response["name"].(string)
		dashboard := response["dashboard_url"].(string)

		options := map[string]string{
			models.ConfigToken.String():         token,
			models.ConfigAPIHost.String():       localConfig.APIHost.Value,
			models.ConfigDashboardHost.String(): dashboard,
		}

		// only set verifytls if using non-default value
		if !verifyTLS {
			options[models.ConfigVerifyTLS.String()] = localConfig.VerifyTLS.Value
		}

		configuration.Set(configuration.Scope, options)

		utils.Log("")
		utils.Log(fmt.Sprintf("Welcome, %s", name))

		if prevConfig.Token.Value != "" {
			prevScope, err1 := filepath.Abs(prevConfig.Token.Scope)
			newScope, err2 := filepath.Abs(configuration.Scope)
			if err1 == nil && err2 == nil && prevScope == newScope {
				utils.LogDebug("Revoking previous token")
				_, err := http.RevokeAuthToken(prevConfig.APIHost.Value, utils.GetBool(prevConfig.VerifyTLS.Value, verifyTLS), prevConfig.Token.Value)
				if !err.IsNil() {
					utils.LogDebug("Failed to revoke token")
				} else {
					utils.LogDebug("Token successfully revoked")
				}
			}
		}
	},
}

var loginRollCmd = &cobra.Command{
	Use:   "roll",
	Short: "Roll your auth token",
	Long: `Roll your auth token

This will generate a new token and revoke the old one.
Your saved configuration will be updated.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		localConfig := configuration.LocalConfig(cmd)
		updateConfig := !utils.GetBoolFlag(cmd, "no-update-config")

		utils.RequireValue("token", localConfig.Token.Value)

		oldToken := localConfig.Token.Value

		response, err := http.RollAuthToken(localConfig.APIHost.Value, utils.GetBool(localConfig.VerifyTLS.Value, true), oldToken)
		if !err.IsNil() {
			utils.HandleError(err.Unwrap(), err.Message)
		}

		newToken := response["token"].(string)

		if updateConfig {
			// update token in config
			for scope, config := range configuration.AllConfigs() {
				if config.Token == oldToken {
					configuration.Set(scope, map[string]string{models.ConfigToken.String(): newToken})
				}
			}
		}

		utils.Log("Auth token has been rolled")
	},
}

var loginRevokeCmd = &cobra.Command{
	Use:   "revoke",
	Short: "Revoke your auth token",
	Long: `Revoke your auth token

Your auth token will be immediately revoked.
This is an alias of the "logout" command.`,
	Args: cobra.NoArgs,
	Run:  revokeToken,
}

func init() {
	loginCmd.Flags().Bool("no-copy", false, "do not copy the auth code to the clipboard")
	loginCmd.Flags().String("scope", "/", "the directory to scope your token to")
	loginCmd.Flags().Bool("overwrite", false, "overwrite existing token if one exists")
	loginCmd.Flags().BoolP("yes", "y", false, "open browser without confirmation")

	loginRollCmd.Flags().String("scope", "/", "the directory to scope your token to")
	loginRollCmd.Flags().Bool("no-update-config", false, "do not update the rolled token in the config file")
	loginCmd.AddCommand(loginRollCmd)

	loginRevokeCmd.Flags().String("scope", "/", "the directory to scope your token to")
	loginRevokeCmd.Flags().Bool("no-update-config", false, "do not remove the revoked token and Enclave configuration from the config file")
	loginRevokeCmd.Flags().Bool("no-update-enclave-config", false, "do not remove the Enclave configuration from the config file")
	loginRevokeCmd.Flags().BoolP("yes", "y", false, "proceed without confirmation")
	loginCmd.AddCommand(loginRevokeCmd)

	rootCmd.AddCommand(loginCmd)
}
