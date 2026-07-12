package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"regexp"

	goplist "github.com/fork2fix/go-plist"
	//goplist "github.com/DHowett/go-plist"
)

// maxPlistSize caps how large an Info.plist entry we are willing to read
// from an IPA. Real-world Info.plist files are a few KiB to a few dozen
// KiB; anything bigger is either malicious or broken. The previous code
// did make([]byte, plfile.FileInfo().Size()), so a crafted IPA declaring
// a 10 GiB uncompressed size would trigger a 10 GiB allocation before
// any read happened.
const maxPlistSize = 16 * 1024 * 1024 // 16 MiB

func parseIpaIcon(path string) (data []byte, err error) {
	iconPattern := regexp.MustCompile(`(?i)^Payload/[^/]*/icon\.png$`)
	r, err := zip.OpenReader(path)
	if err != nil {
		return
	}
	defer r.Close()

	var zfile *zip.File
	for _, file := range r.File {
		if iconPattern.MatchString(file.Name) {
			zfile = file
			break
		}
	}
	if zfile == nil {
		err = errors.New("icon.png file not found")
		return
	}
	plreader, err := zfile.Open()
	if err != nil {
		return
	}
	defer plreader.Close()
	return io.ReadAll(plreader)
}

func parseIPA(path string) (plinfo *plistBundle, err error) {
	plistre := regexp.MustCompile(`^Payload/[^/]*/Info\.plist$`)
	r, err := zip.OpenReader(path)
	if err != nil {
		return
	}
	defer r.Close()

	var plfile *zip.File
	for _, file := range r.File {
		if plistre.MatchString(file.Name) {
			plfile = file
			break
		}
	}
	if plfile == nil {
		err = errors.New("Info.plist file not found")
		return
	}
	plreader, err := plfile.Open()
	if err != nil {
		return
	}
	defer plreader.Close()
	// Guard against a crafted IPA declaring a huge uncompressed size:
	// make([]byte, size) would allocate that much up front. Cap the
	// allocation at maxPlistSize and reject anything bigger.
	declared := plfile.FileInfo().Size()
	if declared > maxPlistSize {
		err = fmt.Errorf("plist entry too large: %d bytes (max %d)", declared, maxPlistSize)
		return
	}
	buf := make([]byte, declared)
	_, err = io.ReadFull(plreader, buf)
	if err != nil {
		return
	}
	dec := goplist.NewDecoder(bytes.NewReader(buf))
	plinfo = new(plistBundle)
	err = dec.Decode(plinfo)
	return
}

type plistBundle struct {
	CFBundleIdentifier  string `plist:"CFBundleIdentifier"`
	CFBundleVersion     string `plist:"CFBundleVersion"`
	CFBundleDisplayName string `plist:"CFBundleDisplayName"`
	CFBundleName        string `plist:"CFBundleName"`
	CFBundleIconFile    string `plist:"CFBundleIconFile"`
	CFBundleIcons       struct {
		CFBundlePrimaryIcon struct {
			CFBundleIconFiles []string `plist:"CFBundleIconFiles"`
		} `plist:"CFBundlePrimaryIcon"`
	} `plist:"CFBundleIcons"`
}

// ref: https://gist.github.com/frischmilch/b15d81eabb67925642bd#file_manifest.plist
type plAsset struct {
	Kind string `plist:"kind"`
	URL  string `plist:"url"`
}

type plItem struct {
	Assets   []*plAsset `plist:"assets"`
	Metadata struct {
		BundleIdentifier string `plist:"bundle-identifier"`
		BundleVersion    string `plist:"bundle-version"`
		Kind             string `plist:"kind"`
		Title            string `plist:"title"`
	} `plist:"metadata"`
}

type downloadPlist struct {
	Items []*plItem `plist:"items"`
}

func generateDownloadPlist(baseURL *url.URL, ipaPath string, plinfo *plistBundle) ([]byte, error) {
	dp := new(downloadPlist)
	item := new(plItem)
	baseURL.Path = ipaPath
	ipaUrl := baseURL.String()
	item.Assets = append(item.Assets, &plAsset{
		Kind: "software-package",
		URL:  ipaUrl,
	})

	iconFiles := plinfo.CFBundleIcons.CFBundlePrimaryIcon.CFBundleIconFiles
	if iconFiles != nil && len(iconFiles) > 0 {
		baseURL.Path = "/-/unzip/" + ipaPath + "/-/**/" + iconFiles[0] + ".png"
		imgUrl := baseURL.String()
		item.Assets = append(item.Assets, &plAsset{
			Kind: "display-image",
			URL:  imgUrl,
		})
	}

	item.Metadata.Kind = "software"

	item.Metadata.BundleIdentifier = plinfo.CFBundleIdentifier
	item.Metadata.BundleVersion = plinfo.CFBundleVersion
	item.Metadata.Title = plinfo.CFBundleName
	if item.Metadata.Title == "" {
		item.Metadata.Title = filepath.Base(ipaUrl)
	}

	dp.Items = append(dp.Items, item)
	data, err := goplist.MarshalIndent(dp, goplist.XMLFormat, "    ")
	return data, err
}
