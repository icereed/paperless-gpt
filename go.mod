module paperless-gpt

go 1.24.4

toolchain go1.25.5

require (
	cloud.google.com/go/documentai v1.39.0
	github.com/Masterminds/sprig/v3 v3.3.0
	github.com/disintegration/imaging v1.6.2
	github.com/fatih/color v1.18.0
	github.com/gabriel-vasile/mimetype v1.4.12
	github.com/gardar/ocrchestra v0.0.0-20250521145628-aaae7e4d40e9
	github.com/gen2brain/go-fitz v1.24.15
	github.com/gin-gonic/gin v1.11.0
	github.com/google/uuid v1.6.0
	github.com/hashicorp/go-retryablehttp v0.7.8
	github.com/pdfcpu/pdfcpu v0.11.1
	github.com/sirupsen/logrus v1.9.3
	github.com/stretchr/testify v1.11.1
	github.com/tmc/langchaingo v0.1.14
	golang.org/x/sync v0.19.0
	golang.org/x/time v0.14.0
	google.golang.org/api v0.257.0
	google.golang.org/genai v1.46.0
	gorm.io/driver/sqlite v1.6.0
	gorm.io/gorm v1.31.1
)

require (
	cloud.google.com/go v0.120.0 // indirect
	cloud.google.com/go/auth v0.17.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	cloud.google.com/go/longrunning v0.6.7 // indirect
	codeberg.org/go-pdf/fpdf v0.11.0 // indirect
	dario.cat/mergo v1.0.2 // indirect
	github.com/Masterminds/goutils v1.1.1 // indirect
	github.com/Masterminds/semver/v3 v3.3.1 // indirect
	github.com/bytedance/sonic v1.14.0 // indirect
	github.com/bytedance/sonic/loader v0.3.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.2.0 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/ebitengine/purego v0.8.4 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/gage-technologies/mistral-go v1.1.0 // indirect
	github.com/gin-contrib/sse v1.1.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.27.0 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/goccy/go-yaml v1.18.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.7 // indirect
	github.com/googleapis/gax-go/v2 v2.15.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hhrutter/lzw v1.0.0 // indirect
	github.com/hhrutter/pkcs7 v0.2.0 // indirect
	github.com/hhrutter/tiff v1.0.2 // indirect
	github.com/huandu/xstrings v1.5.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/jupiterrider/ffi v0.5.0 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/mattn/go-sqlite3 v1.14.24 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/phpdave11/gofpdi v1.0.13 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkoukk/tiktoken-go v0.1.7 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/quic-go/qpack v0.5.1 // indirect
	github.com/quic-go/quic-go v0.54.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.3.0 // indirect
	gitlab.com/golang-commonmark/html v0.0.0-20191124015941-a22733972181 // indirect
	gitlab.com/golang-commonmark/linkify v0.0.0-20191026162114-a0c2df6c8f82 // indirect
	gitlab.com/golang-commonmark/markdown v0.0.0-20211110145824-bf3e522c626a // indirect
	gitlab.com/golang-commonmark/mdurl v0.0.0-20191124015652-932350d1cb84 // indirect
	gitlab.com/golang-commonmark/puny v0.0.0-20191124015043-9f83538fa04f // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.61.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.61.0 // indirect
	go.opentelemetry.io/otel v1.38.0 // indirect
	go.opentelemetry.io/otel/metric v1.38.0 // indirect
	go.opentelemetry.io/otel/trace v1.38.0 // indirect
	go.uber.org/mock v0.5.0 // indirect
	golang.org/x/arch v0.20.0 // indirect
	golang.org/x/crypto v0.45.0 // indirect
	golang.org/x/image v0.32.0 // indirect
	golang.org/x/mod v0.29.0 // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/oauth2 v0.33.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	golang.org/x/tools v0.38.0 // indirect
	google.golang.org/genproto v0.0.0-20250603155806-513f23925822 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20251022142026-3a174f9686a8 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251124214823-79d6a2a48846 // indirect
	google.golang.org/grpc v1.77.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
