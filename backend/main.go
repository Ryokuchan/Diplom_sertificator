package main

import (
	"log"
	"os"

	"diploma-verify/db"
	"diploma-verify/handlers"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file, using environment variables")
	}

	if os.Getenv("DIPLOMA_SECRET_KEY") == "" {
		log.Fatal("DIPLOMA_SECRET_KEY is not set")
	}

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
