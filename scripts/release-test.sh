export GITHUB_TOKEN=$(printf "protocol=https\nhost=github.com\n" | git credential fill | awk -F= '$1 == "password" { print $2 }')
goreleaser release --clean --skip validate
# goreleaser release --clean