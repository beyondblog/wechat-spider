package spider

import (
	"encoding/json"
	"github.com/PuerkitoBio/goquery"
	"github.com/robertkrimen/otto"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

type WechatSpiderTask struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Status     int           `json:"status"`
	TTL        time.Duration `json:"ttl"`
	Timestamp  int64         `json:"timestamp"`
	UpdateTime int64         `json:"updateTime"`
}

type WechatSpiderResult struct {
	ID        string          `json:"id"`
	Data      []WechatArticle `json:"data"`
	Timestamp int64           `json:"timestamp"`
}

type WechatArticle struct {
	Title     string `json:"title"`
	Url       string `json:"url"`
	Thumbnail string `json:"thumbnail"`
	Date      int    `json:"date"`
	SourceUrl string `json:"source_url"`
	Digest    string `json:"digest"`
}

func Spider(wechatName string, proxyUrl *url.URL) ([]WechatArticle, error) {
	timeout := time.Duration(20 * time.Second) //超时时间
	var client *http.Client
	if proxyUrl != nil {
		client = &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)}}
	} else {
		client = &http.Client{Timeout: timeout}
	}

	profile, cookie := getProfile(client, wechatName)
	if profile == "" {
		return nil, nil
	}
	log.Println(profile)

	req, err := http.NewRequest("GET", profile, nil)

	for _, c := range cookie {
		req.AddCookie(c)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_9_2) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/34.0.1847.116 Safari/537.36")
	req.Header.Set("Referer", "http://weixin.sogou.com/weixin?type=1&query="+wechatName+"&ie=utf8&_sug_=n&_sug_type_=")

	resp, err := client.Do(req)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	log.Println(string(body))

	r, _ := regexp.Compile("<script[\\s\\S]*?>([\\s\\S]*?)</script>")
	result := r.FindAllStringSubmatch(string(body), -1)

	//初始化一些垃圾玩意
	js := "window = {}; location={};document = {};getQueryFromURL = function() {return {}};seajs = {}; seajs.use = function() {}"
	for i := range result {
		if i == 2 || i == 7 {
			js += result[i][1]
		}
	}

	vm := otto.New()

	js += `
		   var obj = [];
	       var lists = JSON.parse(msgList.html()).list
		   lists.forEach(function(item) {
			   var msg = item.app_msg_ext_info;
			   obj.push({date: item.comm_msg_info.datetime , title: msg.title, thumbnail: msg.cover, url: "http://mp.weixin.qq.com" +  msg.content_url.html(), digest: msg.digest, source_url: msg.source_url})
		   });

		   var result = JSON.stringify(obj);

	`
	if _, err := vm.Run(js); err != nil {
		return nil, err
	}

	val, e := vm.Get("result")
	if e != nil {
		return nil, e
	}

	articles := []WechatArticle{}
	json.Unmarshal([]byte(val.String()), &articles)
	return articles, nil
}

func getProfile(client *http.Client, name string) (string, []*http.Cookie) {
	req, err := http.NewRequest("GET", "http://weixin.sogou.com/weixin?type=1&query="+name+"&ie=utf8&_sug_=n&_sug_type_=", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_9_2) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/34.0.1847.116 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		log.Println(err)
		return "", nil
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromResponse(resp)

	if err != nil {
		log.Println(err)
		return "", nil
	}

	profile := ""

	doc.Find(".wx-rb").EachWithBreak(func(i int, s *goquery.Selection) bool {
		// For each item found, get the band and title
		weixinhao := s.Find("label[name='em_weixinhao']").Text()
		if weixinhao == name {
			val, _ := s.Attr("href")
			profile = val
			return false
		}
		return true
	})

	return profile, resp.Cookies()
}
