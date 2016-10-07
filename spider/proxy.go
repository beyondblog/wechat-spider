package spider

import (
	"github.com/coreos/etcd/client"
	"golang.org/x/net/context"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

//从代理服务器取 ip
func getProxyList() []string {
	resp, err := http.Get("")
	if err != nil {
		log.Println(err)
		return nil
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	return strings.Split(string(body), "\r\n")
}

//更新代理服务
func UpdateProxyList(api client.KeysAPI) {
	log.Println("更新代理服务")
	proxys := getProxyList()
	log.Println("获取代理列表成功!")
	proxyAvail := make([]string, 0)

	var wg sync.WaitGroup

	for _, proxy := range proxys {
		proxyUrl, _ := url.Parse("http://" + proxy)
		wg.Add(1)
		go func(proxyUrl *url.URL) {
			defer wg.Done()

			log.Println("尝试:" + proxyUrl.String())
			_, err := Spider("renrendai", proxyUrl)

			if err == nil {
				log.Println("代理可用:", proxyUrl.Host)
				proxyAvail = append(proxyAvail, proxyUrl.Host)
			}
		}(proxyUrl)
	}

	wg.Wait()
	log.Printf("可用代理数量:%d\n", len(proxyAvail))

	if len(proxyAvail) > 0 {

		for _, p := range proxyAvail {
			api.Set(context.Background(), "/spider/proxy/"+p, "", &client.SetOptions{
				TTL: time.Second * 600, //有效时间10分钟
			})
		}
	}

}
