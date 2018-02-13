// Copyright © 2013-2018 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

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
	"sync"

	"github.com/wtolson/go-taglib"
	"github.com/yookoala/realpath"
)

var visitedDstCovers = struct {
	v map[dstCoverKey]bool
	sync.RWMutex
}{v: map[dstCoverKey]bool{}}

// transformer applies the changes resulting from the script run.
// If the audio stream needs to be transcoded, it calls FFmpeg to apply all the changes.
// Otherwise, it copies / renames the file and changes metadata with TagLib if necessary.
type transformer struct{}

func (t *transformer) Init() {}

func (t *transformer) Close() {}

func (t *transformer) Run(fr *FileRecord) error {
	input := &fr.input

	for track := 0; track < input.trackCount; track++ {
		output := &fr.output[track]

		if fr.status[track] == statusFail {
			continue
		}

		err := os.MkdirAll(filepath.Dir(output.Path), 0777)
		if err != nil {
			fr.error.Print(err)
			continue
		}

		// Create file if necessary.
		if fr.status[track] == statusExist {
			// If output.Path == input.path && output.Removesource, we process
			// in-place.
			if output.Write == existWriteSkip && (output.Path != input.path || !output.Removesource) {
				if output.Removesource {
					// If the user has explicitly requested WriteSkip and
					// Removesource, it's probably because the exisintg files
					// have priority over the input files.
					fr.info.Printf("Remove source %q", input.path)
					err := os.Remove(input.path)
					if err != nil {
						fr.error.Println(err)
						return err
					}
				}
				continue
			} else if output.Write == existWriteSuffix && (!output.Removesource || output.Path != input.path) {
				output.Path, err = mkTemp(output.Path)
				if err != nil {
					fr.error.Print(err)
					continue
				}
			} else if output.Write == existWriteOver && !output.Removesource && output.Path == input.path {
				continue
			}

		} else {
			// 'output.Path' does not exist.
			st, err := os.Stat(input.path)
			if err != nil {
				fr.error.Print(err)
				// This error will probably happen for the remaining files of the loop.
				// Let's return now.
				return err
			}

			f, err := os.OpenFile(output.Path, os.O_CREATE|os.O_EXCL, st.Mode())
			if err != nil {
				// Either the parent folder is not writable, or a race condition happened:
				// another file with the same path was created between existence check and
				// creation.
				fr.error.Print(err)
				continue
			}
			f.Close()
		}

		// If encoding changed, use FFmpeg. Otherwise, copy/rename the file to
		// speed up the process. If tags have changed but not the encoding, we use
		// taglib to set them.
		var encodingChanged = false

		if input.trackCount > 1 {
			// Split cue-sheet.
			encodingChanged = true
		}

		if fr.Format.FormatName != output.Format {
			encodingChanged = true
		}

		if len(output.Parameters) != 2 ||
			output.Parameters[0] != "-c:a" ||
			output.Parameters[1] != "copy" {
			encodingChanged = true
		}

		// TODO: TagLib does not support arbitrary tags from its C interface.
		// It can tag inplace which offers a significant speedup. The
		// 'taglibSupported' is a workaround used to check whether FFmpeg should be
		// used or not to ensure correct results.
		var taglibFormats = map[string]bool{
			"album":   true,
			"artist":  true,
			"comment": true,
			"genre":   true,
			"title":   true,
			// 'date' and 'track' are handled separately because TagLib only supports
			// integers for those tags.
		}
		var taglibSupported = true
		for k, v := range input.tags {
			if k != "encoder" && output.Tags[k] != v {
				if k == "date" || k == "track" {
					if _, err := strconv.Atoi(v); err != nil {
						taglibSupported = false
						break
					}
				} else if !taglibFormats[k] {
					taglibSupported = false
					break
				}
			}
		}

		if taglibSupported {
			for k, v := range output.Tags {
				if k != "encoder" && input.tags[k] != v {
					if k == "date" || k == "track" {
						if _, err := strconv.Atoi(v); err != nil {
							taglibSupported = false
							break
						}
					} else if !taglibFormats[k] {
						taglibSupported = false
						break
					}
				}
			}
		}

		// Copy embeddedCovers, externalCovers and onlineCover.
		// We must process covers now because the input file can be removed after audio processing.
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

		// TODO: Add to condition: `|| output.format == "taglib-unsupported-format"`.
		if encodingChanged || !taglibSupported {
			err = transformStream(fr, track)
		} else {
			err = transformMetadata(fr, track)
		}
		if err != nil {
			fr.error.Print(err)
			continue
		}
	}

	return nil
}

func transformStream(fr *FileRecord, track int) error {
	input := &fr.input
	output := &fr.output[track]

	// Store encoding parameters.
	ffmpegParameters := []string{}

	// Be verbose only when running a single process. Otherwise output gets
	// would get messy.
	if options.Cores > 1 {
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
		d, _ := strconv.ParseFloat(fr.Streams[input.audioIndex].Duration, 64)
		start, duration := ffmpegSplitTimes(input.cuesheet, input.cuesheetFile, track, d)
		ffmpegParameters = append(ffmpegParameters, "-ss", start, "-t", duration)
	}

	// If there are no covers, do not copy any video stream to avoid errors.
	if fr.Format.NbStreams < 2 {
		ffmpegParameters = append(ffmpegParameters, "-vn")
	}

	// Remove non-cover streams and extra audio streams.
	// Must add all streams first.
	ffmpegParameters = append(ffmpegParameters, "-map", "0")
	for i := 0; i < fr.Format.NbStreams; i++ {
		if (fr.Streams[i].CodecType == "video" && fr.Streams[i].CodecName != "image2" && fr.Streams[i].CodecName != "png" && fr.Streams[i].CodecName != "mjpeg") ||
			(fr.Streams[i].CodecType == "audio" && i > input.audioIndex) ||
			(fr.Streams[i].CodecType != "audio" && fr.Streams[i].CodecType != "video") {
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
	dst := output.Path
	if input.path == output.Path {
		var err error
		dst, err = mkTemp(output.Path)
		if err != nil {
			fr.error.Print(err)
			return err
		}
	}
	ffmpegParameters = append(ffmpegParameters, dst)

	fr.debug.Printf("FFmpeg parameters: track #%v %q", track, ffmpegParameters)

	cmd := exec.Command("ffmpeg", ffmpegParameters...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		fr.error.Printf(stderr.String())
		return err
	}

	if input.path == output.Path {
		fr.debug.Printf("Rename %q to %q to transform inplace", dst, input.path)
		err = os.Rename(dst, input.path)
		if err != nil {
			fr.error.Print(err)
			return err
		}
	} else if output.Removesource {
		fr.info.Printf("Remove source %q", input.path)
		err := os.Remove(input.path)
		if err != nil {
			fr.error.Println(err)
			return err
		}
	}

	return nil
}

func transformMetadata(fr *FileRecord, track int) error {
	input := &fr.input
	output := &fr.output[track]

	var err error

	if input.path != output.Path {
		// Rename or copy file.
		if output.Removesource {
			fr.debug.Printf("Rename %q to %q", input.path, output.Path)
			err = os.Rename(input.path, output.Path)
		}
		if err != nil || !output.Removesource {
			// If renaming failed, it might be because of a cross-device
			// destination. We try to copy instead.
			fr.debug.Printf("Copy %q to %q", input.path, output.Path)
			err := CopyFile(output.Path, input.path)
			if err != nil {
				fr.error.Println(err)
				return err
			}
			if output.Removesource {
				fr.debug.Printf("Remove source %q", input.path)
				err = os.Remove(input.path)
				if err != nil {
					fr.error.Println(err)
				}
			}
		}
	}

	var tagsChanged = false

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

	if tagsChanged {
		fr.debug.Print("Set tags with TagLib")

		f, err := taglib.Read(output.Path)
		if err != nil {
			fr.error.Print(err)
			return err
		}
		defer f.Close()

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
			t, _ := strconv.Atoi(output.Tags["track"])
			// There is no need to check for errors as the caller has already.
			f.SetTrack(t)
		}
		if output.Tags["date"] != "" {
			t, _ := strconv.Atoi(output.Tags["date"])
			// There is no need to check for errors as the caller has already.
			f.SetYear(t)
		}

		err = f.Save()
		if err != nil {
			fr.error.Print(err)
		}
	}
	return nil
}

// mkTemp creates a temp file by appending a random suffix to 'dst' while
// preserving its extension. Return the name of the temp file.
func mkTemp(dst string) (temp string, err error) {
	f, err := TempFile(filepath.Dir(dst), StripExt(filepath.Base(dst))+"_", "."+Ext(dst))
	if err != nil {
		return "", err
	}
	temp = f.Name()
	f.Close()
	return temp, nil
}

// Create a new destination file 'dst'. See makeTrackDst.
// As a special case, if the checksums match in input and dst, return "", nil.
// TODO: Test how memoization scales with visitedDstCovers.
func makeCoverDst(fr *FileRecord, dst string, inputPath string, checksum string) (string, error) {
	if st, err := os.Stat(dst); err == nil || !os.IsNotExist(err) {
		// 'dst' exists.

		// Realpath is required for cache key uniqueness.
		dst, err = realpath.Realpath(dst)
		if err != nil {
			return "", err
		}

		visitedDstCovers.RLock()
		visited := visitedDstCovers.v[dstCoverKey{path: dst, checksum: checksum}]
		visitedDstCovers.RUnlock()
		if visited {
			return "", nil
		}
		visitedDstCovers.Lock()
		visitedDstCovers.v[dstCoverKey{path: dst, checksum: checksum}] = true
		visitedDstCovers.Unlock()

		// Compute checksum of existing cover and early-out if equal.
		fd, err := os.Open(dst)
		if err != nil {
			return "", err
		}
		defer fd.Close()

		// TODO: Cache checksums.
		hi := st.Size()
		if hi > coverChecksumBlock {
			hi = coverChecksumBlock
		}

		buf := [coverChecksumBlock]byte{}
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
		visitedDstCovers.Lock()
		visitedDstCovers.v[dstCoverKey{path: dst, checksum: checksum}] = true
		visitedDstCovers.Unlock()
	}

	return dst, nil
}

func transferCovers(fr *FileRecord, cover outputCover, coverName string, inputSource io.Reader, checksum string) {
	if cover.Path == "" {
		return
	}

	if len(cover.Parameters) == 0 || cover.Format == "" {
		coverNewPath, err := makeCoverDst(fr, cover.Path, fr.input.path, checksum)
		if err != nil {
			fr.error.Print(err)
			return
		}
		if coverNewPath == "" {
			fr.debug.Printf("Cover %s skipped, identical file exists in %v", coverName, cover.Path)
			return
		}

		fd, err := os.OpenFile(coverNewPath, os.O_WRONLY|os.O_TRUNC, 0666)
		if err != nil {
			fr.warning.Println(err)
			return
		}

		fr.info.Printf("Cover %v -> %s", coverName, coverNewPath)
		if _, err = io.Copy(fd, inputSource); err != nil {
			fr.warning.Println(err)
			return
		}
		fd.Close()

	} else {
		coverNewPath, err := makeCoverDst(fr, cover.Path, fr.input.path, checksum)
		if err != nil {
			fr.error.Print(err)
			return
		}
		if coverNewPath == "" {
			fr.debug.Printf("Cover %s skipped, identical file exists in %v", coverName, cover.Path)
			return
		}

		cmdArray := []string{"-nostdin", "-v", "error", "-y", "-i", "-", "-an", "-sn"}
		cmdArray = append(cmdArray, cover.Parameters...)
		cmdArray = append(cmdArray, "-f", cover.Format, coverNewPath)

		fr.info.Printf("Cover %v -> %s", coverName, coverNewPath)
		fr.debug.Printf("FFmpeg parameters: %q", cmdArray)

		cmd := exec.Command("ffmpeg", cmdArray...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		cmd.Stdin = inputSource

		_, err = cmd.Output()
		if err != nil {
			fr.warning.Printf(stderr.String())
			return
		}
	}
}
