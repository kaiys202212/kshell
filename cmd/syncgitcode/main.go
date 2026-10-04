package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/yangk/kshell/internal/update"
)

func main() {
	dir := flag.String("dir", "dist", "含 zip 与 SHA256SUMS 的目录")
	tag := flag.String("tag", os.Getenv("TAG"), "版本 tag")
	flag.Parse()
	err := update.SyncGitCode(context.Background(), update.SyncOptions{
		Token: os.Getenv("GITCODE_TOKEN"),
		Owner: os.Getenv("GITCODE_OWNER"),
		Repo:  os.Getenv("GITCODE_REPO"),
		Tag:   *tag,
		Dir:   *dir,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "syncgitcode:", err)
		os.Exit(1)
	}
}
