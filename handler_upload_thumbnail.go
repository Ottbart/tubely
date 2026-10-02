package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// TODO: implement the upload here
	const maxMemory = 10 << 20
	if err = r.ParseMultipartForm(maxMemory); err != nil {
		respondWithError(w, http.StatusBadRequest, "Error passing data", err)
		return
	}

	// "thumbnail" should match the HTML form input name
	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	// check the media type of the file
	contentType := header.Header.Get("Content-Type")

	/*
		data, err := io.ReadAll(file)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "Unable to read file data", err)
			return
		}
	*/

	// check if the user is the owner of the video
	meta, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to get video ID from database", err)
		return
	}
	if meta.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "user is not owner of requested video", errors.New("user ID did not match"))
		return
	}

	// check extension of the file
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid file extension", err)
		return
	}
	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type. Only JPEG and PNG are allowed", nil)
		return
	}

	// build the path to save the file
	extension := strings.Split(mediaType, "/")
	randKey := make([]byte, 32)
	if _, err := rand.Read(randKey); err != nil {
		respondWithError(w, http.StatusInternalServerError, "error generating filename: ", err)
		return
	}
	filename := base64.RawURLEncoding.EncodeToString(randKey) + "." + extension[1]
	thumbnailPath := filepath.Join(cfg.assetsRoot, filename)
	localFile, err := os.Create(thumbnailPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error creating file: ", err)
		return
	}
	defer localFile.Close()

	// write the file to disk
	if _, err := io.Copy(localFile, file); err != nil {
		respondWithError(w, http.StatusInternalServerError, "error writing file", err)
		return
	}
	// update the video metadata with the thumbnail path and save it to the database
	thumbnailURL := fmt.Sprintf("http://localhost:%s/%s", cfg.port, thumbnailPath)
	meta.ThumbnailURL = &thumbnailURL
	if err := cfg.db.UpdateVideo(meta); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, meta)
}
