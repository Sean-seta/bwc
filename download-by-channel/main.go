package main

import (
	"bwc/model"
	"database/sql"
	"fmt"
	"gorm.io/gorm/clause"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	bucket = "s3-vdr-amazon-delivery-or-1" // replace with your S3 bucket name
	dsn    = "host=34.132.252.7 user=user dbname=postgres port=6432 sslmode=disable TimeZone=Asia/Shanghai"
)

func connectDB() *gorm.DB {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to DB:", err)
	}
	//db.AutoMigrate(&model.Video{})
	return db
}

// ---------- Download using yt-dlp ----------
func downloadVideo(url, prefix string, version string) error {
	fmt.Printf("Downloading videos from %s with prefix %s and version %s\n", url, prefix, version)
	outputTemplate := "%(id)s.%(ext)s"

	// If version is v1, add "_v2" suffix to base name
	if version == "v1" {
		outputTemplate = "%(id)s_v2.%(ext)s"
	}
	cookiePath := "./download-by-channel/cookies_fpt_phong"
	// yt-dlp placeholders:
	//   {}            -> path to downloaded media file (e.g. abc123.mp4)
	//   %(infojson)s  -> path to the info.json file
	// We'll upload both files, then remove them.
	execCmd := fmt.Sprintf(`
dest="s3://%s/%s"

echo "[exec] Uploading all .mp4 and matching .info.json files to $dest"

for v in *.mp4; do
  if [ -f "$v" ]; then
    base=$(basename "$v" .mp4)
    j="${base}.info.json"

    echo "[exec] Uploading video: $v"
    aws s3 cp "$v" "$dest/"
    if [ $? -eq 0 ]; then
      echo "[exec] Removing local video: $v"
      rm -f "$v"
    else
      echo "[exec] Failed to upload video: $v (not removed)"
    fi

    if [ -f "$j" ]; then
      echo "[exec] Uploading info: $j"
      aws s3 cp "$j" "$dest/"
      if [ $? -eq 0 ]; then
        echo "[exec] Removing local info: $j"
        rm -f "$j"
      else
        echo "[exec] Failed to upload info: $j (not removed)"
      fi
    fi
  fi
done
`, bucket, prefix)

	cmd := exec.Command(
		"yt-dlp",
		"--cookies", cookiePath,
		"--match-filter", "duration>120",
		"--write-info-json",
		"-o", outputTemplate,
		"--sleep-requests", "2",
		"--sleep-interval", "3",
		"--extractor-args", "youtube:formats=duplicate",
		"-f", "bv[height>=720][height<=1080]+ba/best[ext=mp4][height>=720][height<=1080]",
		"--remux-video", "mp4",
		"--exec", execCmd,
		url,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("yt-dlp failed: %w", err)
	}

	return nil
}

// ---------- Upload to S3 ----------
func uploadToS3(file string, prefix string) (string, error) {
	base := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	info := base + ".info.json"

	// Upload video
	fmt.Printf("Upload to S3: s3://%s/%s/%s\n", bucket, prefix, filepath.Base(file))
	runCmd("aws", "s3", "cp", "--", file, fmt.Sprintf("s3://%s/%s/%s", bucket, prefix, filepath.Base(file)))

	// Upload info JSON
	fmt.Printf("Upload to S3: s3://%s/%s/%s\n", bucket, prefix, filepath.Base(info))
	runCmd("aws", "s3", "cp", "--", info, fmt.Sprintf("s3://%s/%s/%s", bucket, prefix, filepath.Base(info)))

	// Clean up local files safely
	for _, f := range []string{file, info} {
		if err := os.Remove(f); err != nil {
			fmt.Println("Warning: failed to delete file:", f, err)
		}
	}

	return fmt.Sprintf("https://%s.s3.amazonaws.com/%s/%s", bucket, prefix, filepath.Base(file)), nil
}

func runCmd(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		log.Fatal("Command failed:", err, "Command:", name, strings.Join(args, " "))
	}
}

func mustOpen(path string) *os.File {
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	return f
}

// ---------- Worker ----------
func processPendingVideos(db *gorm.DB) {
	for {
		var video model.Video

		// Start transaction
		err := db.Transaction(func(tx *gorm.DB) error {
			// Get one pending video with lock, skip already locked rows
			result := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
				Where("status = ?", model.VideoStatusPending).
				First(&video)
			if result.Error != nil {
				if result.Error == gorm.ErrRecordNotFound {
					fmt.Println("No more pending videos. Sleeping...")
					time.Sleep(5 * time.Second)
					return nil
				}
				return result.Error
			}

			// Update status to cooking inside the transaction
			err := tx.Model(&model.Video{}).Where("id = ?", video.ID).Updates(map[string]interface{}{
				"status":   model.VideoStatusCooking,
				"start_at": time.Now(),
			}).Error
			if err != nil {
				return err
			}

			return nil
		})

		if err != nil {
			log.Println("DB transaction error:", err)
			time.Sleep(2 * time.Second)
			continue
		}

		// Continue with your existing logic
		err = downloadVideo(video.URL, string(video.Prefix), video.Version)
		if err != nil {
			fmt.Println("❌ Download failed:", err)
			//db.Model(&video).Update("status", model.VideoStatusFailed)
			continue
		}

		// Upload
		//s3url, err := uploadToS3(filePath, string(video.Prefix))
		//if err != nil {
		//	fmt.Println("❌ S3 upload failed:", err)
		//	db.Model(&video).Update("status", model.VideoStatusFailed)
		//	continue
		//}

		// Update DB
		err = db.Model(&model.Video{}).
			Where("id = ?", video.ID).
			Updates(model.Video{
				Status: model.VideoStatusCompleted,
				EndAt:  time.Now(),
			}).Error
		if err != nil {
			log.Println("Failed to update video status:", err)
			continue
		}

		time.Sleep(2 * time.Second)
	}
}

func main() {
	checkYtDlpVersion()

	db := connectDB()
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatal("Failed to get SQL DB:", err)
	}
	err = sqlDB.Ping()
	if err != nil {
		log.Fatal("Failed to ping DB:", err)
	}
	defer func(sqlDB *sql.DB) {
		err = sqlDB.Close()
		if err != nil {
			fmt.Println("Failed to close DB:", err)
		}
	}(sqlDB)

	//db.AutoMigrate(&model.Video{})
	processPendingVideos(db)
}
func checkYtDlpVersion() {
	cmd := exec.Command("yt-dlp", "--version")
	out, err := cmd.Output()
	if err != nil {
		log.Println("Failed to get yt-dlp version:", err)
		return
	}
	fmt.Println("yt-dlp version:", strings.TrimSpace(string(out)))
}
