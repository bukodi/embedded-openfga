package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"net/http"
	"os"
)

var (
	githubOauthConfig = &oauth2.Config{
		ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		RedirectURL:  "http://localhost:8007/callback",
		Scopes:       []string{"user:email"},
		Endpoint:     github.Endpoint, // use the GitHub OAUTH2 endpoint, in this demo we use mock oauth server
	}
	oauthStateString = "random" // Use a secure random value in production
)

type Email struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

func getUserEmails(c *gin.Context, accessToken string) ([]Email, error) {
	mockServerURL := c.MustGet("mockServer")
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/user/emails", mockServerURL), nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Println("Error closing response body:", err)
		}
	}()

	var emails []Email
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return nil, err
	}
	return emails, nil
}

func main() {
	mockServer := mockOAuthServer()
	defer mockServer.Close()

	githubOauthConfig.Endpoint = oauth2.Endpoint{
		AuthURL:  mockServer.URL + "/authorize",
		TokenURL: mockServer.URL + "/token",
	}

	if os.Getenv("INITIAL_TUPLES") == "" {
		panic("INITIAL_TUPLES environment variable is not set")
	}
	var tuples []Tuple
	if err := json.Unmarshal([]byte(os.Getenv("INITIAL_TUPLES")), &tuples); err != nil {
		panic(errors.Wrap(err, "failed to unmarshal INITIAL_TUPLES environment variable"))
	}
	logger, err := zap.NewDevelopment(zap.IncreaseLevel(zap.DebugLevel))
	if err != nil {
		panic(errors.Wrap(err, "failed to initialize zap logger"))
	}
	openFgaServer, err := NewOpenFGA(
		os.Getenv("DATASTORE_URI"),
		WithInitialTuples(tuples),
		WithModelFile(os.Getenv("MODEL_FILE")),
		WithStoreName(os.Getenv("STORE_NAME")),
		WithAuthorizationModelName(os.Getenv("AUTHORIZATION_MODEL_NAME")),
		WithLogger(logger),
	)
	if err != nil {
		fmt.Println("Failed to initialize OpenFGA server:", err)
		return
	}

	r := gin.Default()
	r.LoadHTMLGlob("templates/*")

	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.tmpl", gin.H{
			"title": "Embedded OpenFGA",
		})
	})

	r.GET("/login", func(c *gin.Context) {
		url := githubOauthConfig.AuthCodeURL(oauthStateString)
		c.Redirect(http.StatusTemporaryRedirect, url)
	})

	r.GET("/callback", func(c *gin.Context) {
		state := c.Query("state")
		if state != oauthStateString {
			c.String(http.StatusBadRequest, "State mismatch")
			return
		}
		code := c.Query("code")
		token, err := githubOauthConfig.Exchange(context.Background(), code)
		if err != nil {
			c.String(http.StatusInternalServerError, "Code exchange failed: %s", err.Error())
			return
		}
		//scopes, _ := token.Extra("scope").(string)
		c.Set("mockServer", mockServer.URL)
		emails, err := getUserEmails(c, token.AccessToken)
		if err != nil {
			c.String(http.StatusInternalServerError, "Failed to get user emails: %s", err.Error())
			return
		}
		if len(emails) == 0 {
			c.String(http.StatusInternalServerError, "No emails found for the user")
			return
		}

		c.SetCookie("user", emails[0].Email, 3600, "/", "localhost", false, true)
		c.Redirect(http.StatusTemporaryRedirect, "/documents")
	})

	r.GET("/documents", func(c *gin.Context) {
		// allow all logged-in users to view documents
		userEmail, err := c.Cookie("user")
		if err != nil {
			fmt.Println("Error retrieving user cookie:", err)
			c.Redirect(http.StatusTemporaryRedirect, "/")
		}
		if userEmail == "" {
			fmt.Println("User cookie is empty, redirecting to home")
			c.Redirect(http.StatusTemporaryRedirect, "/")
		}
		c.HTML(http.StatusOK, "documents.tmpl", gin.H{
			"title": "Documents",
			"documents": map[string]map[string]string{
				"1": {"id": "1", "name": "Document 1"},
				"2": {"id": "2", "name": "Document 2"},
			},
		})
	})

	r.GET("/document/:docID/view", func(c *gin.Context) {
		docID := c.Param("docID")
		userEmail, err := c.Cookie("user")
		if err != nil {
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": "You must be logged in to view this document.",
			})
			return
		}
		// Policy Decision Point (PDP) check

		allowed, err1 := openFgaServer.Check(c.Request.Context(), Tuple{Object: "document:" + docID, Relation: "viewer", User: "user:" + userEmail})

		// Policy Enforcement Point (PEP) check
		if err1 != nil {
			fmt.Println("user:"+userEmail, "err:", err1)
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": fmt.Sprintf("User %s is not allowed to view document %s", userEmail, docID),
			})
			return
		}
		if !allowed {
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": fmt.Sprintf("User %s is not allowed to view document %s", userEmail, docID),
			})
			return
		}
		c.HTML(http.StatusOK, "document.tmpl", gin.H{
			"title":  "Document View",
			"user":   userEmail,
			"docID":  docID,
			"action": "viewing",
		})
	})

	r.GET("/document/:docID/edit", func(c *gin.Context) {
		docID := c.Param("docID")
		userEmail, err := c.Cookie("user")
		if err != nil {
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": "You must be logged in to view this document.",
			})
			return
		}
		// Policy Decision Point (PDP) check
		allowed, err1 := openFgaServer.Check(c.Request.Context(), Tuple{Object: "document:" + docID, Relation: "editor", User: "user:" + userEmail})

		// Policy Enforcement Point (PEP) check
		if err1 != nil {
			fmt.Println("user:"+userEmail, "err:", err1)
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": fmt.Sprintf("User %s is not allowed to edit document %s", userEmail, docID),
			})
			return
		}
		if !allowed {
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": fmt.Sprintf("User %s is not allowed to view document %s", userEmail, docID),
			})
			return
		}
		c.HTML(http.StatusOK, "document.tmpl", gin.H{
			"title":  "Document Edit",
			"user":   userEmail,
			"docID":  docID,
			"action": "editing",
		})
	})
	r.GET("/admin", func(c *gin.Context) {
		userEmail, err := c.Cookie("user")
		if err != nil {
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": "You must be logged in to access the admin panel.",
			})
			return
		}
		if userEmail == "" {
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": "You must be logged in to access the admin panel.",
			})
			return
		}
		// Policy Decision Point (PDP) check

		allowed, err1 := openFgaServer.Check(c.Request.Context(), Tuple{Object: "app:auth", Relation: "admin", User: "user:" + userEmail})
		// Policy Enforcement Point (PEP) check
		if err1 != nil {
			fmt.Println("user:"+userEmail, "err:", err1)
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": fmt.Sprintf("User %s is not allowed to access the admin panel", userEmail),
			})
			return
		}
		if !allowed {
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": fmt.Sprintf("User %s is not allowed to access the admin panel", userEmail),
			})
			return
		}
		c.HTML(http.StatusOK, "admin.tmpl", gin.H{
			"title": "Admin Panel",
		})
	})
	r.POST("/admin/add-tuple", func(c *gin.Context) {
		userEmail, err := c.Cookie("user")
		if err != nil {
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": "You must be logged in to access the admin panel.",
			})
			return
		}
		if userEmail == "" {
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": "You must be logged in to access the admin panel.",
			})
			return
		}
		// Policy Decision Point (PDP) check
		allowed, err1 := openFgaServer.Check(c.Request.Context(), Tuple{Object: "app:auth", Relation: "admin", User: "user:" + userEmail})
		// Policy Enforcement Point (PEP) check
		if err1 != nil {
			fmt.Println("user:"+userEmail, "err:", err1)
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": fmt.Sprintf("User %s is not allowed to access the admin panel", userEmail),
			})
			return
		}
		if !allowed {
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": fmt.Sprintf("User %s is not allowed to access the admin panel", userEmail),
			})
			return
		}
		// Policy Administration Point (PAP) operation
		// TODO the user input needs to be validated and sanitized
		objectID := c.PostForm("document")
		relation := c.PostForm("relation")
		userID := c.PostForm("user")
		err = openFgaServer.Write(c.Request.Context(), []Tuple{{
			Object:   "document:" + objectID,
			Relation: relation,
			User:     "user:" + userID,
		}})
		if err != nil {
			fmt.Println("Error writing tuple:", err)
			c.HTML(http.StatusInternalServerError, "error.tmpl", gin.H{
				"title":   "Error",
				"message": "Failed to add tuple.",
			})
			return
		}
		c.Redirect(http.StatusSeeOther, "/documents")
	})
	err = r.Run(":8007")
	if err != nil {
		panic(err)
	}
}
