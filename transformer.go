package main

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/wtolson/go-taglib"
	"github.com/yookoala/realpath"
)

type transformer struct{}

func (t *transformer) Init() {
}

func (t *transformer) Close() {
}

func (t *transformer) Run(fr *FileRecord) error {
	input := &fr.input

	// Re-encode / copy / rename.
	for track := 0; track < input.trackCount; track++ {
		output := &fr.output[track]

		err := os.MkdirAll(filepath.Dir(output.Path), 0777)
		if err != nil {
			fr.Error.Print(err)
			return err
		}

		// Copy embeddedCovers, externalCovers and onlineCover.
		for stream, cover := range output.EmbeddedCovers {
			inputSource := bytes.NewBuffer(fr.embeddedCoverCache[stream])
			transferCovers(fr, cover, "embedded "+strconv.Itoa(stream), inputSource, input.embeddedCovers[stream].checksum)
		}
		for file, cover := range output.ExternalCovers {
			inputPath := filepath.Join(filepath.Dir(input.path), file)
			inputSource, err := os.Open(inputPath)
			if err != nil {
				return err
			}
			transferCovers(fr, cover, "external '"+file+"'", inputSource, input.externalCovers[file].checksum)
			inputSource.Close()
		}
		{
			inputSource := bytes.NewBuffer(fr.onlineCoverCache)
			transferCovers(fr, output.OnlineCover, "online", inputSource, input.onlineCover.checksum)
		}

		// If encoding changed, use FFmpeg. Otherwise, copy/rename the file to
		// speed up the process. If tags have changed but not the encoding, we use
		// taglib to set them.
		var encodingChanged = false
		var tagsChanged = false

		if input.trackCount > 1 {
			// Split cue-sheet.
			encodingChanged = true
		}

		if input.Format.Format_name != output.Format {
			encodingChanged = true
		}

		if len(output.Parameters) != 2 ||
			output.Parameters[0] != "-c:a" ||
			output.Parameters[1] != "copy" {
			encodingChanged = true
		}

		// Test if tags have changed.
		for k, v := range input.tags {
			if k != "encoder" && output.Tags[k] != v {
				tagsChanged = true
				break
			}
		}
		if !tagsChanged {
			for k, v := range output.Tags {
				if k != "encoder" && input.tags[k] != v {
					tagsChanged = true
					break
				}
			}
		}

		// TODO: Add to condition: `|| output.format == "taglib-unsupported-format"`.
		if encodingChanged {
			return saveTranscode(fr, track)
		} else {
			return saveKeepStream(fr, track, tagsChanged)
		}
	}

	return nil
}

func saveTranscode(fr *FileRecord, track int) error {
	input := &fr.input
	output := &fr.output[track]

	// Store encoding parameters.
	ffmpegParameters := []string{}

	// Be verbose only when running a single process. Otherwise output gets
	// would get messy.
	if OPTIONS.cores > 1 {
		ffmpegParameters = append(ffmpegParameters, "-v", "warning")
	} else {
		ffmpegParameters = append(ffmpegParameters, "-v", "error")
	}

	// By default, FFmpeg reads stdin while running. Disable this feature to
	// avoid unexpected problems.
	ffmpegParameters = append(ffmpegParameters, "-nostdin")

	// FFmpeg should always overwrite: if a temp file is created to avoid
	// overwriting, FFmpeg should clobber it.
	ffmpegParameters = append(ffmpegParameters, "-y")

	ffmpegParameters = append(ffmpegParameters, "-i", input.path)

	// Stream codec.
	ffmpegParameters = append(ffmpegParameters, output.Parameters...)

	// Get cuesheet splitting parameters.
	if len(input.cuesheet.Files) > 0 {
		d, _ := strconv.ParseFloat(input.Streams[input.audioIndex].Duration, 64)
		start, duration := FFmpegSplitTimes(input.cuesheet, input.cuesheetFile, track, d)
		ffmpegParameters = append(ffmpegParameters, "-ss", start, "-t", duration)
	}

	// If there are no covers, do not copy any video stream to avoid errors.
	if input.Format.Nb_streams < 2 {
		ffmpegParameters = append(ffmpegParameters, "-vn")
	}

	// Remove non-cover streams and extra audio streams.
	// Must add all streams first.
	ffmpegParameters = append(ffmpegParameters, "-map", "0")
	for i := 0; i < input.Format.Nb_streams; i++ {
		if (input.Streams[i].Codec_type == "video" && input.Streams[i].Codec_name != "image2" && input.Streams[i].Codec_name != "png" && input.Streams[i].Codec_name != "mjpeg") ||
			(input.Streams[i].Codec_type == "audio" && i > input.audioIndex) ||
			(input.Streams[i].Codec_type != "audio" && input.Streams[i].Codec_type != "video") {
			ffmpegParameters = append(ffmpegParameters, "-map", "-0:"+strconv.Itoa(i))
		}
	}

	// Remove subtitles if any.
	ffmpegParameters = append(ffmpegParameters, "-sn")

	// '-map_metadata -1' clears all metadata first.
	ffmpegParameters = append(ffmpegParameters, "-map_metadata", "-1")

	for tag, value := range output.Tags {
		ffmpegParameters = append(ffmpegParameters, "-metadata", tag+"="+value)
	}

	// Format.
	ffmpegParameters = append(ffmpegParameters, "-f", output.Format)

	// Output file.
	// FFmpeg cannot transcode inplace, so we force creating a temp file if
	// necessary.
	dst, isInplace, err := makeTrackDst(output.Path, input.path, false)
	if err != nil {
		fr.Error.Print(err)
		return err
	}
	ffmpegParameters = append(ffmpegParameters, dst)

	fr.Debug.Printf("Audio %v parameters: %q", track, ffmpegParameters)

	cmd := exec.Command("ffmpeg", ffmpegParameters...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		fr.Error.Printf(stderr.String())
		return err
	}

	if OPTIONS.removesource {
		if err != nil {
			fr.Error.Print(err)
			return err
		}
		if isInplace {
			err = os.Rename(dst, input.path)
			if err != nil {
				fr.Error.Print(err)
			}
		} else {
			err = os.Remove(input.path)
			if err != nil {
				fr.Error.Print(err)
			}
		}
	}

	return nil
}

func saveKeepStream(fr *FileRecord, track int, tagsChanged bool) error {
	input := &fr.input
	output := &fr.output[track]

	dst, isInplace, err := makeTrackDst(output.Path, input.path, OPTIONS.removesource)
	if err != nil {
		fr.Error.Print(err)
		return err
	}

	if !isInplace {
		err = nil
		if OPTIONS.removesource {
			err = os.Rename(input.path, dst)
		}
		if err != nil || !OPTIONS.removesource {
			// If renaming failed, it might be because of a cross-device
			// destination. We try to copy instead.
			err := CopyFile(dst, input.path)
			if err != nil {
				fr.Error.Println(err)
				return err
			}
			if OPTIONS.removesource {
				err = os.Remove(input.path)
				if err != nil {
					fr.Error.Println(err)
				}
			}
		}
	}

	if tagsChanged {
		// TODO: Can TagLib remove extra tags?
		f, err := taglib.Read(dst)
		if err != nil {
			fr.Error.Print(err)
			return err
		}
		defer f.Close()

		// TODO: Arbitrary tag support with taglib?
		if output.Tags["album"] != "" {
			f.SetAlbum(output.Tags["album"])
		}
		if output.Tags["artist"] != "" {
			f.SetArtist(output.Tags["artist"])
		}
		if output.Tags["comment"] != "" {
			f.SetComment(output.Tags["comment"])
		}
		if output.Tags["genre"] != "" {
			f.SetGenre(output.Tags["genre"])
		}
		if output.Tags["title"] != "" {
			f.SetTitle(output.Tags["title"])
		}
		if output.Tags["track"] != "" {
			t, err := strconv.Atoi(output.Tags["track"])
			if err == nil {
				f.SetTrack(t)
			}
		}
		if output.Tags["date"] != "" {
			t, err := strconv.Atoi(output.Tags["date"])
			if err == nil {
				f.SetYear(t)
			}
		}

		err = f.Save()
		if err != nil {
			fr.Error.Print(err)
		}
	}
	return nil
}

// Create a new destination file 'dst'.
//
// As an additional informational value, it says if the real paths of outputPath
// and inputPath are the same. It saves the need for recomputing that value
// later on.
//
// As a special case, if 'inputPath == dst' and 'removesource == true',
// then modify the file inplace.
// If no third-party program overwrites existing files, this approach cannot
// clobber existing files.
func makeTrackDst(outputPath string, inputPath string, removeSource bool) (dst string, isInplace bool, err error) {
	if _, err := os.Stat(outputPath); err == nil || !os.IsNotExist(err) {
		// 'outputPath' exists.
		// The realpath is required to see if transformation is inplace.
		// The realpath can only be expanded when the parent folder exists.
		dst, err = realpath.Realpath(outputPath)
		if err != nil {
			return "", false, err
		}

		if inputPath == dst {
			isInplace = true
		} else {
			if !removeSource {
				// If not inplace, create a temp file.
				f, err := TempFile(filepath.Dir(dst), StripExt(filepath.Base(dst))+"_", "."+Ext(dst))
				if err != nil {
					return "", false, err
				}
				dst = f.Name()
				f.Close()
			}
		}
	} else {
		// 'outputPath' does not exist.
		st, err := os.Stat(inputPath)
		if err != nil {
			return "", false, err
		}

		f, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL, st.Mode())
		if err != nil {
			// Either the parent folder is not writable, or a race condition happened:
			// another file with the same path was created between existence check and
			// creation.
			return "", false, err
		}
		f.Close()
		dst = outputPath
	}

	return dst, false, nil
}

// Create a new destination file 'dst'. See makeTrackDst.
// As a special case, if the checksums match in input and dst, return "", nil.
// TODO: Test how memoization scales with VISITED_DST_COVERS.
func makeCoverDst(fr *FileRecord, dst string, inputPath string, checksum string) (string, error) {
	if st, err := os.Stat(dst); err == nil || !os.IsNotExist(err) {
		// 'dst' exists.

		// Realpath is required for cache key uniqueness.
		dst, err = realpath.Realpath(dst)
		if err != nil {
			return "", err
		}

		VISITED_DST_COVERS.RLock()
		visited := VISITED_DST_COVERS.v[dstCoverKey{path: dst, checksum: checksum}]
		VISITED_DST_COVERS.RUnlock()
		if visited {
			return "", nil
		}
		VISITED_DST_COVERS.Lock()
		VISITED_DST_COVERS.v[dstCoverKey{path: dst, checksum: checksum}] = true
		VISITED_DST_COVERS.Unlock()

		// Compute checksum of existing cover and early-out if equal.
		fd, err := os.Open(dst)
		if err != nil {
			return "", err
		}
		defer fd.Close()

		// TODO: Cache checksums.
		hi := st.Size()
		if hi > COVER_CHECKSUM_BLOCK {
			hi = COVER_CHECKSUM_BLOCK
		}

		buf := [COVER_CHECKSUM_BLOCK]byte{}
		_, err = (*fd).ReadAt(buf[:hi], 0)
		if err != nil && err != io.EOF {
			return "", err
		}
		dstChecksum := fmt.Sprintf("%x", md5.Sum(buf[:hi]))

		if checksum == dstChecksum {
			return "", nil
		}

		// If not inplace, create a temp file.
		f, err := TempFile(filepath.Dir(dst), StripExt(filepath.Base(dst))+"_", "."+Ext(dst))
		if err != nil {
			return "", err
		}
		dst = f.Name()
		f.Close()
	} else {
		// 'dst' does not exist.

		st, err := os.Stat(inputPath)
		if err != nil {
			return "", err
		}

		fd, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL, st.Mode())
		if err != nil {
			// Either the parent folder is not writable, or a race condition happened:
			// file was created between existence check and file creation.
			return "", err
		}
		fd.Close()

		// Save to cache.
		dst, err = realpath.Realpath(dst)
		if err != nil {
			return "", err
		}
		VISITED_DST_COVERS.Lock()
		VISITED_DST_COVERS.v[dstCoverKey{path: dst, checksum: checksum}] = true
		VISITED_DST_COVERS.Unlock()
	}

	return dst, nil
}

func transferCovers(fr *FileRecord, cover outputCover, coverName string, inputSource io.Reader, checksum string) {
	var err error
	if cover.Path == "" {
		return
	}
	if len(cover.Parameters) == 0 || cover.Format == "" {
		cover.Path, err = makeCoverDst(fr, cover.Path, fr.input.path, checksum)
		if err != nil {
			fr.Error.Print(err)
			return
		}
		if cover.Path == "" {
			// Identical file exists.
			return
		}

		fd, err := os.OpenFile(cover.Path, os.O_WRONLY|os.O_TRUNC, 0666)
		if err != nil {
			fr.Warning.Println(err)
			return
		}

		if _, err = io.Copy(fd, inputSource); err != nil {
			fr.Warning.Println(err)
			return
		}
		fd.Close()

	} else {
		cover.Path, err = makeCoverDst(fr, cover.Path, fr.input.path, checksum)
		if err != nil {
			fr.Error.Print(err)
			return
		}
		if cover.Path == "" {
			// Identical file exists.
			return
		}

		cmdArray := []string{"-nostdin", "-v", "error", "-y", "-i", "-", "-an", "-sn"}
		cmdArray = append(cmdArray, cover.Parameters...)
		cmdArray = append(cmdArray, "-f", cover.Format, cover.Path)

		fr.Debug.Printf("Cover %v parameters: %q", coverName, cmdArray)

		cmd := exec.Command("ffmpeg", cmdArray...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		cmd.Stdin = inputSource

		_, err := cmd.Output()
		if err != nil {
			fr.Warning.Printf(stderr.String())
			return
		}
	}
}
