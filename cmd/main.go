package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/tuple"
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
	defer resp.Body.Close()

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

	openFgaServer, err := InitOpenFGA()
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
		fmt.Println("document:"+docID, "view", "user:"+userEmail)
		v, err1 := openFgaServer.Server.Check(context.Background(), &openfgav1.CheckRequest{
			StoreId:              openFgaServer.StoreID,
			AuthorizationModelId: openFgaServer.AuthorizationModelId,
			TupleKey:             tuple.NewCheckRequestTupleKey("document:"+docID, "viewer", "user:"+userEmail),
		})

		// Policy Enforcement Point (PEP) check
		if err1 != nil {
			fmt.Println("user:"+userEmail, "err:", err1)
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": fmt.Sprintf("User %s is not allowed to view document %s", userEmail, docID),
			})
			return
		}
		if !v.GetAllowed() {
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
		v, err1 := openFgaServer.Server.Check(context.Background(), &openfgav1.CheckRequest{
			StoreId:              openFgaServer.StoreID,
			AuthorizationModelId: openFgaServer.AuthorizationModelId,
			TupleKey:             tuple.NewCheckRequestTupleKey("document:"+docID, "editor", "user:"+userEmail),
		})

		// Policy Enforcement Point (PEP) check
		if err1 != nil {
			fmt.Println("user:"+userEmail, "err:", err1)
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": fmt.Sprintf("User %s is not allowed to edit document %s", userEmail, docID),
			})
			return
		}
		if !v.GetAllowed() {
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
		v, err1 := openFgaServer.Server.Check(context.Background(), &openfgav1.CheckRequest{
			StoreId:              openFgaServer.StoreID,
			AuthorizationModelId: openFgaServer.AuthorizationModelId,
			TupleKey:             tuple.NewCheckRequestTupleKey("app:auth", "admin", "user:"+userEmail),
		})
		// Policy Enforcement Point (PEP) check
		if err1 != nil {
			fmt.Println("user:"+userEmail, "err:", err1)
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": fmt.Sprintf("User %s is not allowed to access the admin panel", userEmail),
			})
			return
		}
		if !v.GetAllowed() {
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
		v, err1 := openFgaServer.Server.Check(c.Request.Context(), &openfgav1.CheckRequest{
			StoreId:              openFgaServer.StoreID,
			AuthorizationModelId: openFgaServer.AuthorizationModelId,
			TupleKey:             tuple.NewCheckRequestTupleKey("app:auth", "admin", "user:"+userEmail),
		})
		// Policy Enforcement Point (PEP) check
		if err1 != nil {
			fmt.Println("user:"+userEmail, "err:", err1)
			c.HTML(http.StatusUnauthorized, "auth-error.tmpl", gin.H{
				"title":   "Authentication Error",
				"message": fmt.Sprintf("User %s is not allowed to access the admin panel", userEmail),
			})
			return
		}
		if !v.GetAllowed() {
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
		_, err = openFgaServer.Server.Write(c.Request.Context(), &openfgav1.WriteRequest{
			StoreId:              openFgaServer.StoreID,
			AuthorizationModelId: openFgaServer.AuthorizationModelId,
			Writes: &openfgav1.WriteRequestWrites{
				TupleKeys: []*openfgav1.TupleKey{
					tuple.NewTupleKey("document:"+objectID, relation, "user:"+userID),
				},
			},
		})
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
	r.Run(":8007")
}
