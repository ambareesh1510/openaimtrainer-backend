package main

import (
	"fmt"
	"strconv"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"mime/multipart"
	// "time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/BurntSushi/toml"
)

func main() {
	app := pocketbase.New()

	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		collectionName := "scenarioMetadata"

		// Check if the collection already exists
		collection, err := app.FindCollectionByNameOrId(collectionName)
		if err == nil && collection != nil {
			// Already exists — do nothing
			return nil
		}

		newCollection := core.NewBaseCollection(collectionName)
		// TODO: make rules non-nil
		newCollection.ViewRule = nil
		newCollection.CreateRule = nil
		newCollection.UpdateRule = nil

		newCollection.Fields.Add(&core.TextField{
			Name: "name",
			Required: true,
		})

		newCollection.Fields.Add(&core.TextField{
			Name: "author",
			Required: true,
		})

		fieldMin := 0.0
		newCollection.Fields.Add(&core.NumberField{
			Name: "time",
			Required: true,
			Min: &fieldMin,
		})

		newCollection.Fields.Add(&core.TextField{
			Name: "uuid",
			Required: true,
		})

		newCollection.Fields.Add(&core.DateField{
			Name: "created",
			Required: true,
		})

		usersCollection, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}

		newCollection.Fields.Add(&core.RelationField{
			Name: "createdBy",
			Required: true,
			CascadeDelete: false,
			CollectionId: usersCollection.Id,
		})

		newCollection.AddIndex("idx_scenarios_unique_name", true, "name", "")
		if err = app.Save(newCollection); err != nil {
			return err
		}

		log.Println("Created 'scenarios' collection at startup")

		usersCollection.AddIndex("idx_users_unique_name", true, "name", "")
		if err = app.Save(usersCollection); err != nil {
			return err
		}

		log.Println("Created unique index on users ('name')")

		return nil

	})

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		router := se.Router

		// Serve static files from pb_public
		router.GET("/{path...}", apis.Static(os.DirFS("./pb_public"), false))

		router.POST("/api/signup", func(e *core.RequestEvent) error {
			// time.Sleep(2 * time.Second)
			collection, err := app.FindCollectionByNameOrId("users")
			if err != nil {
				return err
			}

			info, err := e.RequestInfo()
			if err != nil {
				return err
			}

			// TODO: check that username is unique
			usernameText, ok := info.Body["username"].(string)
			if !ok || usernameText == "" {
				return echo.NewHTTPError(
					http.StatusBadRequest,
					"Invalid value for username",
				)
			}

			emailText, ok := info.Body["email"].(string)
			if !ok || emailText == "" {
				return echo.NewHTTPError(
					http.StatusBadRequest,
					"Invalid value for email",
				)
			}

			passwordText, ok := info.Body["password"].(string)
			if !ok || passwordText == "" {
				return echo.NewHTTPError(
					http.StatusBadRequest,
					"Invalid value for password",
				)
			}

			record := core.NewRecord(collection)
			record.Set("name", usernameText)
			record.SetEmail(emailText)
			record.SetPassword(passwordText)
			// TODO: add email verification
			record.SetVerified(true)
			if err := app.Save(record); err != nil {
				return err
			}

			return e.JSON(http.StatusOK, echo.Map{
				"message":         "Registered user",
				"email":  	emailText,
			})
		})

		router.POST("/api/login", func(e *core.RequestEvent) error {
			// time.Sleep(2 * time.Second)
			info, err := e.RequestInfo()
			if err != nil {
				return err
			}

			emailText, ok := info.Body["email"].(string)
			if !ok || emailText == "" {
				return echo.NewHTTPError(
					http.StatusBadRequest,
					"Invalid value for email",
				)
			}

			passwordText, ok := info.Body["password"].(string)
			if !ok || passwordText == "" {
				return echo.NewHTTPError(
					http.StatusBadRequest,
					"Invalid value for password",
				)
			}

			record, err := app.FindFirstRecordByData("users", "email", emailText)
			if err != nil {
				return echo.NewHTTPError(
					http.StatusBadRequest,
					"User not found",
				)
			}

			passwordValid := record.ValidatePassword(passwordText)
			if !passwordValid {
				return echo.NewHTTPError(
					http.StatusUnauthorized,
					"Incorrect password",
				)
			}

			username := record.GetString("name")
			
			token, err := record.NewAuthToken()
			if err != nil {
				return err
			}
			return e.JSON(http.StatusOK, echo.Map{
				"token": token,
				"username": username,
			})
		})

		// TODO: rate limit with redis
		router.POST("/api/createScenario", func(e *core.RequestEvent) error {
			// time.Sleep(2 * time.Second)
			authHeader := e.Request.Header.Get("Authorization")
			if authHeader == "" {
				return echo.NewHTTPError(401, "Missing Authorization header")
			}

			authRecord, err := app.FindAuthRecordByToken(authHeader)
			if err != nil {
				return err
			}

			collection, err := app.FindCollectionByNameOrId("scenarioMetadata")
			if err != nil {
				return err
			}

			// Get uploaded files
			infoFile, _, err := e.Request.FormFile("info.toml")
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "Missing info.toml")
			}
			defer infoFile.Close()

			scriptFile, _, err := e.Request.FormFile("script.lua")
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "Missing script.lua")
			}
			defer scriptFile.Close()

			info, err := e.RequestInfo()
			if err != nil {
				return err
			}

			type InfoToml struct {
				Name string `toml:"name"`
				Author string `toml:"author"`
				// Description string `toml:"description"`
				Time float64 `toml:"time"`
				// ApiVersion string `toml:"api_version"`
			}

			infoData, err := io.ReadAll(infoFile)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to read info.toml")
			}
			var parsedInfoData InfoToml
			_, err = toml.Decode(string(infoData), &parsedInfoData)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to parse info.toml")
			}

			time, err := strconv.ParseFloat(info.Body["time"].(string), 64)
			if err != nil {
				return err
			}

			if parsedInfoData.Name != info.Body["name"] || parsedInfoData.Author != info.Body["author"] || parsedInfoData.Time != time {
				return echo.NewHTTPError(http.StatusInternalServerError, "Supplied metadata and metadata in info.toml do not match")
			}

			record := core.NewRecord(collection)
			record.Set("name", info.Body["name"].(string))
			record.Set("author", info.Body["author"].(string))
			record.Set("time", time)
			record.Set("created", types.NowDateTime())
			record.Set("createdBy", authRecord.Id)

			// Generate a unique ID for this scenario
			scenarioID := uuid.New().String()
			record.Set("uuid", scenarioID)
			if err := app.Save(record); err != nil {
				return err
			}

			// TODO: if the files don't get saved, remove the record
			destDir := filepath.Join("pb_public", "scenarios", scenarioID)

			if err := os.MkdirAll(destDir, 0755); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create directory")
			}

			// Save info.toml
			infoDest := filepath.Join(destDir, "info.toml")
			if err := saveUploadedFile(infoFile, infoDest); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save info.toml")
			}

			// Save script.lua
			scriptDest := filepath.Join(destDir, "script.lua")
			if err := saveUploadedFile(scriptFile, scriptDest); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save script.lua")
			}


			// Return success
			return e.JSON(http.StatusOK, echo.Map{
				"id":         scenarioID,
				"info_file":  fmt.Sprintf("/scenarios/%s/info.toml", scenarioID),
				"script_file": fmt.Sprintf("/scenarios/%s/script.lua", scenarioID),
			})
		})

		router.POST("/api/findScenarios", func(e *core.RequestEvent) error {
			// time.Sleep(2 * time.Second)
			info, err := e.RequestInfo()
			if err != nil {
				return err
			}

			var records []*core.Record
			// var err any
			query := info.Body["query"]
			if query == "" {
				records, err = app.FindRecordsByFilter(
					"scenarioMetadata",
					"",
					"-created",
					50, 0,
				)
				if err != nil {
					return err
				}
			} else {
				records, err = app.FindRecordsByFilter(
					"scenarioMetadata",
					"name ~ {:query}",
					"-created",
					50, 0,
					dbx.Params{ "query": query },
				)
				if err != nil {
					return err
				}
			}

			// TODO: remove this
			// time.Sleep(0 * time.Second);

			filtered := make([]map[string]interface{}, 0, len(records))
			for _, r := range records {
				filtered = append(filtered, map[string]interface{}{
					"name":    	r.GetString("name"),
					"author":  	r.GetString("author"),
					"time": 	r.GetFloat("time"),
					"uuid":     r.GetString("uuid"),
				})
			}

			return e.JSON(200, filtered)


		})

		return se.Next()
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

// Helper to save uploaded file
func saveUploadedFile(file multipart.File, dstPath string) error {
	out, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, file)
	return err
}

