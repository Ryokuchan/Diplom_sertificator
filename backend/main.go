package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"diploma-verify/db"
	"diploma-verify/handlers"
)

func main() {
	db.Init()

	r := gin.Default()

	r.POST("/upload", handlers.Upload)
	r.GET("/status/:job_id", handlers.JobStatus)
	r.GET("/verify/:hash", handlers.Verify)
	r.GET("/diplomas", handlers.ListDiplomas)

	log.Println("Server running on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
