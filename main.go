package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
)

func main() {
	// Setup structured logging to both stdout and file
	logFile, err := os.OpenFile("ecr-cleanup.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("❌ Failed to open log file: %v", err)
	}
	defer logFile.Close()

	multiWriter := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(multiWriter)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Input variables
	var region string
	var retention int
	var prefixList string
	var dryRunInput string
	var dryRun bool

	// Get user inputs
	fmt.Print("Enter AWS Region (e.g., us-east-1): ")
	fmt.Scanln(&region)

	fmt.Print("Enter retention period in minutes (e.g., 5): ")
	fmt.Scanln(&retention)

	fmt.Print("Enter comma-separated tag prefixes to keep (e.g., latest,dev,main): ")
	fmt.Scanln(&prefixList)

	fmt.Print("Dry-run mode? (yes/no): ")
	fmt.Scanln(&dryRunInput)
	dryRun = strings.ToLower(dryRunInput) == "yes"

	log.Printf("📌 Region: %s | Retention: %d mins | Prefixes: %s | Dry-run: %v",
		region, retention, prefixList, dryRun)

	// AWS session
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
	})
	if err != nil {
		log.Fatalf("❌ Error creating session: %v", err)
	}

	// ECR client
	svc := ecr.New(sess)

	// List repositories
	repos, err := svc.DescribeRepositories(&ecr.DescribeRepositoriesInput{})
	if err != nil {
		log.Fatalf("❌ Failed to list repositories: %v", err)
	}

	if len(repos.Repositories) == 0 {
		log.Println("⚠️ No repositories found in the specified region.")
		return
	}

	prefixes := strings.Split(prefixList, ",")

	// Process each repo
	for _, repo := range repos.Repositories {
		repoName := *repo.RepositoryName
		log.Printf("\n📦 Repository: %s", repoName)

		imageOutput, err := svc.DescribeImages(&ecr.DescribeImagesInput{
			RepositoryName: aws.String(repoName),
		})
		if err != nil {
			log.Printf("⚠️ Error fetching images for %s: %v", repoName, err)
			continue
		}

		if len(imageOutput.ImageDetails) == 0 {
			log.Printf("⚠️ No images found in repository %s", repoName)
			continue
		}

		for _, image := range imageOutput.ImageDetails {
			if image.ImagePushedAt == nil {
				continue
			}

			imageAge := int(time.Since(*image.ImagePushedAt).Minutes())

			if len(image.ImageTags) == 0 {
				log.Printf("🗑️ Untagged image to delete: %s", *image.ImageDigest)
				continue
			}

			if imageAge > retention {
				keep := false
				for _, tag := range image.ImageTags {
					for _, prefix := range prefixes {
						if strings.HasPrefix(*tag, prefix) {
							keep = true
						}
					}
				}

				if keep {
					log.Printf("✅ Retain image (prefix matched): %s", *image.ImageDigest)
				} else {
					log.Printf("🗑️ Old image to delete: %s (Age: %d minutes)", *image.ImageDigest, imageAge)

					if !dryRun {
						_, err := svc.BatchDeleteImage(&ecr.BatchDeleteImageInput{
							RepositoryName: aws.String(repoName),
							ImageIds: []*ecr.ImageIdentifier{
								{ImageDigest: image.ImageDigest},
							},
						})
						if err != nil {
							log.Printf("❌ Error deleting image %s: %v", *image.ImageDigest, err)
						} else {
							log.Printf("✅ Image deleted: %s", *image.ImageDigest)
						}
					} else {
						log.Printf("ℹ️ Dry-run mode: Skipped deletion of image %s", *image.ImageDigest)
					}
				}
			}
		}
	}
}
