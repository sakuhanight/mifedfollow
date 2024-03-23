/*
Copyright © 2024 Tokimine Sakuha <sakuha@tsuitachi.net>

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
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"log"
	"mifedfollow/lib"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"time"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "mifedfollow",
	Short: "misskey fediverse follow tool",
	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },
	Run: func(cmd *cobra.Command, args []string) {
		destHost := viper.GetString("destHost")
		destToken := viper.GetString("destToken")
		srcHost := viper.GetString("srcHost")
		srcToken := viper.GetString("srcToken")
		tlChannel := viper.GetString("channel")

		var following []string

		// フォローするアカウントのiを手に入れる
		iRes, err := lib.Request(fmt.Sprintf("https://%s/api/i", destHost), map[string]interface{}{
			"i": destToken,
		})
		if err != nil {
			zap.S().Fatalf("failed to get i: %+v", err)
			return
		}
		defer iRes.Body.Close()
		var iResJson map[string]interface{}
		err = json.NewDecoder(iRes.Body).Decode(&iResJson)
		if err != nil {
			zap.S().Fatalf("failed to decode i: %+v", err)
			return
		}
		userId := iResJson["id"]

		// フォローするアカウントの現在のフォローリスト取得
		followingRes, err := lib.Request(fmt.Sprintf("https://%s/api/users/following", destHost), map[string]interface{}{
			"userId": userId,
			"limit":  100,
			"i":      destToken,
		})
		if err != nil {
			zap.S().Fatalf("failed to get following: %+v", err)
			return
		}
		defer followingRes.Body.Close()
		var followingJson []struct {
			Followee struct {
				Username string `json:"username"`
				Host     string `json:"destHost"`
			} `json:"followee"`
		}
		err = json.NewDecoder(followingRes.Body).Decode(&followingJson)
		if err != nil {
			zap.S().Fatalf("failed to decode following: %+v", err)
			return
		}
		for _, f := range followingJson {
			following = append(following, fmt.Sprintf("@%s@%s", f.Followee.Username, f.Followee.Host))
		}
		zap.S().Debugf("following: %v", following)
		zap.S().Infof("following %v users", len(following))

		// シグナルハンドラ
		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, os.Interrupt)

		u := url.URL{Scheme: "wss", Host: srcHost, Path: "/streaming"}
		u.RawQuery = fmt.Sprintf("i=%s", srcToken)
		log.Printf("connecting to %s", u.String())

		c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
		if err != nil {
			zap.S().Fatalf("dial failed: %+v", err)
		}
		defer c.Close()

		done := make(chan struct{})

		chId := fmt.Sprintf("%d", time.Now().UnixNano())
		sendMsgMap := map[string]interface{}{
			"type": "connect",
			"body": map[string]interface{}{
				"channel": tlChannel,
				"id":      chId,
			},
		}
		sendMsgJson, _ := json.Marshal(sendMsgMap)
		err = c.WriteMessage(websocket.TextMessage, sendMsgJson)
		if err != nil {
			zap.S().Fatalf("failed channel connect: %v", err)
		}

		go func() {
			defer close(done)
			for {
				mt, message, err := c.ReadMessage()
				if err != nil {
					zap.S().Warnf("read failed: %+v", err)
					break
				}
				zap.S().Debugf("recv: %s, type: %v", message, mt)
				var msg struct {
					Body struct {
						Type string `json:"type"`
						Body struct {
							User struct {
								Username string `json:"username"`
								Host     string `json:"host"`
							} `json:"user"`
						} `json:"body"`
					} `json:"body"`
				}
				err = json.Unmarshal(message, &msg)
				if err != nil {
					zap.S().Fatalf("failed to unmarshal message: %+v", err)
					return
				}
				zap.S().Debugf("msg: %+v", msg)
				if msg.Body.Type == "note" {
					if msg.Body.Body.User.Host == "" {
						msg.Body.Body.User.Host = srcHost
					}
					if !slices.Contains(following, fmt.Sprintf("@%s@%s", msg.Body.Body.User.Username, msg.Body.Body.User.Host)) {
						usershowRes, err := lib.Request(fmt.Sprintf("https://%s/api/users/show", destHost), map[string]interface{}{
							"username": msg.Body.Body.User.Username,
							"host":     msg.Body.Body.User.Host,
						})
						if err != nil {
							zap.S().Fatalf("failed to get usershow: %+v", err)
							return
						}
						var usershow struct {
							Id string `json:"id"`
						}
						err = json.NewDecoder(usershowRes.Body).Decode(&usershow)
						if err != nil {
							zap.S().Fatalf("failed to decode usershow: %+v", err)
							return
						}
						usershowRes.Body.Close()
						followRes, err := lib.Request(fmt.Sprintf("https://%s/api/following/create", destHost), map[string]interface{}{
							"userId": usershow.Id,
							"i":      destToken,
						})
						zap.S().Debugf("response: %+v", followRes)
						if err != nil {
							zap.S().Fatalf("failed to follow: %+v", err)
							return
						}
						followRes.Body.Close()

						zap.S().Infof("followed: @%s@%s", msg.Body.Body.User.Username, msg.Body.Body.User.Host)

						following = append(following, fmt.Sprintf("@%s@%s", msg.Body.Body.User.Username, msg.Body.Body.User.Host))
						zap.S().Infof("following %v users", len(following))
					}
				}
			}
		}()

		for {
			select {
			case <-done:
				return
			case <-interrupt:
				zap.S().Info("interrupt")
				err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				if err != nil {
					zap.S().Errorf("write close: %v", err)
					return
				}
				select {
				case <-done:
				case <-time.After(time.Second):

				}
			}
		}

	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.mifedfollow.yaml)")

	rootCmd.PersistentFlags().Bool("verbose", false, "output debug logs")
	viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	//rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	rootCmd.Flags().String("destHost", "", "destination host")
	viper.BindPFlag("destHost", rootCmd.Flags().Lookup("destHost"))

	rootCmd.Flags().String("destToken", "", "destination token")
	viper.BindPFlag("destToken", rootCmd.Flags().Lookup("destToken"))

	rootCmd.Flags().String("srcHost", "", "source host")
	viper.BindPFlag("srcHost", rootCmd.Flags().Lookup("srcHost"))

	rootCmd.Flags().String("srcToken", "", "source token")
	viper.BindPFlag("srcToken", rootCmd.Flags().Lookup("srcToken"))

	rootCmd.Flags().Int("limit", 100, "following list limit")
	viper.BindPFlag("limit", rootCmd.Flags().Lookup("limit"))

	rootCmd.Flags().String("channel", "globalTimeline", "timeline channel")
	viper.BindPFlag("channel", rootCmd.Flags().Lookup("channel"))
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".mifedfollow" (without extension).
		viper.AddConfigPath(home)
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName(".mifedfollow")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}

	// loger setup
	config := zap.NewProductionConfig()
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncoderConfig = encoderConfig

	if viper.GetBool("verbose") {
		config.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	} else {
		config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	}

	logger, _ := config.Build(zap.AddCallerSkip(1))
	defer logger.Sync()
	zap.ReplaceGlobals(logger)
}
