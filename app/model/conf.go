package model

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cast"
	"github.com/v03413/bepusdt/app/credential"
	"github.com/v03413/go-cache"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var confCache sync.Map
var defaultConf = map[ConfKey]string{
	ApiAppUri:               "",
	RateSyncInterval:        "3600",
	AtomUSDT:                "0.01",
	AtomUSDC:                "0.01",
	AtomTRX:                 "0.01",
	AtomBNB:                 "0.00001",
	AtomETH:                 "0.000001",
	AtomGRAM:                "0.01",
	MonitorMinAmount:        "0.01",
	PaymentMinAmount:        "0.01",
	PaymentMaxAmount:        "99999",
	RpcEndpointTron:         "grpc.trongrid.io:50051",
	RpcEndpointBsc:          "https://binance-smart-chain-public.nodies.app/",
	RpcEndpointSolana:       "https://solana-rpc.publicnode.com/",
	RpcEndpointXlayer:       "https://xlayerrpc.okx.com/",
	RpcEndpointPolygon:      "https://polygon-public.nodies.app/",
	RpcEndpointArbitrum:     "https://arb1.arbitrum.io/rpc",
	RpcEndpointEthereum:     "https://ethereum-public.nodies.app/",
	RpcEndpointBase:         "https://base-public.nodies.app/",
	RpcEndpointAptos:        "https://aptos-rest.publicnode.com/",
	RpcEndpointPlasma:       "https://rpc.plasma.to/",
	RpcGlobalConfigUrlTon:   "https://ton.org/global-config.json",
	NotifyMaxRetry:          "10",
	BlockHeightMaxDiff:      "1000",
	BlockOffsetConfirm:      "0",
	PaymentTimeout:          "1200",     // 20分钟
	PaymentCheckout:         "official", // 官方模板
	PaymentMatchMode:        string(Classic),
	PaymentSupportUrl:       "",
	PaymentLookbackHour:     "3",
	PaymentNetworkSort:      "",
	SystemInstallLock:       "0",
	RateSyncCoingeckoApiUrl: "https://api.coingecko.com",
	RateSyncHistoryDays:     "30",
	MqttTopicPrefix:         "bepusdt",
	HomeRedirectUrl:         "",
}

type Conf struct {
	K ConfKey `gorm:"column:k;type:varchar(32);not null;primaryKey" json:"key"`
	V string  `gorm:"column:v;type:varchar(512);not null" json:"val"`
}

func (c Conf) TableName() string {

	return "bep_conf"
}

func SetK(k ConfKey, v string) {
	if err = Db.Transaction(func(db *gorm.DB) error {
		if err2 := db.Where("k = ?", k).Delete(&Conf{}).Error; err2 != nil {

			return err2
		}
		if err2 := db.Create(&Conf{K: k, V: v}).Error; err2 != nil {

			return err2
		}

		defer RefreshC()

		return nil
	}); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, fmt.Sprintf("设置配置项 %s 错误：%s", k, err.Error()))
	}
}

// SetSecretValues updates credentials without allowing ORM logs to expand
// plaintext values. All supplied values are committed atomically.
func SetSecretValues(values map[ConfKey]string) error {
	db := Db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	if err := db.Transaction(func(tx *gorm.DB) error {
		for key, value := range values {
			if err := tx.Where("k = ?", key).Delete(&Conf{}).Error; err != nil {
				return err
			}
			if err := tx.Create(&Conf{K: key, V: value}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	RefreshC()
	return nil
}

func GetK(k ConfKey) string {
	var row Conf

	var tx = Db.Where("k = ?", k).Limit(1).Find(&row)
	if tx.Error == nil {

		return row.V
	}

	_, _ = fmt.Fprintln(os.Stderr, fmt.Sprintf("获取配置项 %s 错误：%s", k, tx.Error.Error()))

	return ""
}

func GetVs(keys []ConfKey) map[ConfKey]string {
	var rows = make([]Conf, 0)
	Db.Where("k IN ?", keys).Find(&rows)

	var result = make(map[ConfKey]string)
	for _, row := range rows {
		result[row.K] = row.V
	}

	for _, k := range keys {
		if _, ok := result[k]; !ok {
			result[k] = ""
		}
	}

	return result
}

// GetC 从缓存获取配置，适用于高频读取，依赖 RefreshC 刷新缓存
func GetC(k ConfKey) string {
	value, ok := confCache.Load(k)
	if !ok {
		return ""
	}

	return value.(string)
}

func RefreshC() {
	var rows = make([]Conf, 0)
	Db.Find(&rows)

	for _, row := range rows {
		confCache.Store(row.K, row.V)
	}
}

func CheckoutUrl(host, id string) string {
	uri := GetK(ApiAppUri)
	if uri == "" {
		uri = host
	}

	return fmt.Sprintf("%s/pay/checkout/%s", uri, id)
}

func ConfInit() error {
	credentials, err := credential.Generate()
	if err != nil {
		return fmt.Errorf("generate bootstrap credentials: %w", err)
	}
	encrypt, err := bcrypt.GenerateFromPassword([]byte(credentials.Password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash bootstrap password: %w", err)
	}
	var data = map[ConfKey]string{
		ApiAuthToken:  credentials.APIToken,
		AdminSecret:   credentials.AdminSecret,
		AdminSecure:   credentials.AdminPath,
		AdminUsername: credentials.Username,
		AdminPassword: string(encrypt),
	}
	var rows = make([]Conf, 0)
	for k, v := range data {
		rows = append(rows, Conf{K: k, V: v})
	}
	for k, v := range defaultConf {
		rows = append(rows, Conf{K: k, V: v})
	}

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════════════")
	fmt.Println("║  🎉  欢迎使用 BEpusdt  -  首次运行检测，初始化配置完成")
	fmt.Println("╚═══════════════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("┏━━  🔐  后台登录信息 (请立即保存！)")
	fmt.Println("┃")
	fmt.Println("Initial credentials generated; retrieve them from the one-time installation page.")
	fmt.Println("┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("┏━━  🔌  API 对接信息")
	fmt.Println("┃")
	fmt.Println("API credential generated; it will not be written to stdout or logs.")
	fmt.Println("┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("⚠️   重要提示:")
	fmt.Println("    •  以上信息仅显示一次，请务必妥善保存至安全位置")
	fmt.Println("    •  登录密码遗忘可通过 'reset' 命令重置")
	fmt.Println("    •  API 令牌可在网页后台进行修改")
	fmt.Println("    •  建议定期更换密码以确保账户安全")
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════════")
	fmt.Println()

	// Never allow ORM SQL/slow-query logging to expand credential values.
	if err := Db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Create(&rows).Error; err != nil {
		return fmt.Errorf("store bootstrap configuration: %w", err)
	}

	// 数据丢到缓存，前台首次访问时会展示这部分初始化信息；明文密码只这一次保存到缓存，不写入数据库
	cache.Set(string(SystemInstallLock), gin.H{
		"username": credentials.Username,
		"password": credentials.Password,
		"secure":   credentials.AdminPath,
		"token":    credentials.APIToken,
	}, -1)

	return nil
}

func AuthToken() string {

	return GetK(ApiAuthToken)
}

func IsInstalled() bool {
	return GetC(SystemInstallLock) == "1"
}

func InstallLock() {
	SetK(SystemInstallLock, "1")
}

func GetInstallInfo() gin.H {
	if info, ok := cache.Get(string(SystemInstallLock)); ok {

		return info.(gin.H)
	}

	return gin.H{}
}

func GetTronGridApiKeys() []string {
	arr := strings.Split(GetC(RpcEndpointTronGridApiKey), ",")
	keys := make([]string, 0)
	for _, v := range arr {
		if v != "" {
			keys = append(keys, v)
		}
	}

	return keys
}

func FillDefaultConf() {
	var existKeys []string
	Db.Model(&Conf{}).Pluck("k", &existKeys)

	existSet := make(map[ConfKey]struct{}, len(existKeys))
	for _, k := range existKeys {
		existSet[ConfKey(k)] = struct{}{}
	}

	var rows []Conf
	for k, v := range defaultConf {
		if _, ok := existSet[k]; !ok {
			rows = append(rows, Conf{K: k, V: v})
		}
	}
	if len(rows) > 0 {
		Db.Create(&rows)
	}
}

func GetLookbackHour() time.Duration {
	var hour = time.Hour * -1
	var num = cast.ToInt(GetC(PaymentLookbackHour))

	return time.Duration(num) * hour
}
