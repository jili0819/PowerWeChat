package kernel

import (
	"crypto/sha1"
	"encoding/xml"
	"errors"
	"fmt"
	fmt2 "github.com/ArtisanCloud/PowerLibs/v3/fmt"
	"github.com/ArtisanCloud/PowerLibs/v3/http/contract"
	"github.com/ArtisanCloud/PowerLibs/v3/http/helper"
	"github.com/ArtisanCloud/PowerLibs/v3/object"
	"github.com/ArtisanCloud/PowerWeChat/v3/src/kernel"
	"github.com/ArtisanCloud/PowerWeChat/v3/src/kernel/power"
	response2 "github.com/ArtisanCloud/PowerWeChat/v3/src/kernel/response"
	"github.com/ArtisanCloud/PowerWeChat/v3/src/kernel/support"
	"io"
	"log"
	http "net/http"
	"os"
)

type BaseClient struct {
	kernel.BaseClient

	App *ApplicationPaymentInterface
}

func NewBaseClient(app *ApplicationPaymentInterface) (*BaseClient, error) {
	config := (*app).GetConfig()
	baseURI := config.GetString("http.base_uri", "/")

	httpRequest, err := helper.NewRequestHelper(&helper.Config{
		BaseUrl: baseURI,
	})
	if err != nil {
		return nil, err
	}

	client := &BaseClient{
		BaseClient: kernel.BaseClient{
			HttpHelper: httpRequest,
			Signer: &support.SHA256WithRSASigner{
				MchID:               config.GetString("mch_id", ""),
				CertificateSerialNo: config.GetString("serial_no", ""),
				PrivateKeyPath:      config.GetString("key_path", ""),
			},
		},
		App: app,
	}

	// to be setup middleware here
	client.OverrideGetMiddlewares()
	client.RegisterHttpMiddlewares()

	return client, nil

}

func (client *BaseClient) prepends() *object.HashMap {
	return &object.HashMap{}
}

func (client *BaseClient) PlainRequest(endpoint string, params *object.StringMap, method string, options *object.HashMap,
	returnRaw bool, outHeader interface{}, outBody interface{},
) (response *http.Response, err error) {

	//config := (*client.App).GetConfig()
	base := &object.HashMap{}

	// init options
	if options == nil {
		options = &object.HashMap{}
	}

	options = object.MergeHashMap(base, client.prepends(), options)
	options = object.FilterEmptyHashMap(options)

	// check need sign body or not
	signBody := ""
	if "get" != object.Lower(method) {
		signBody, err = object.JsonEncode(options)
		if err != nil {
			return nil, err
		}
	}

	authorization, err := client.Signer.GenerateRequestSign(&support.RequestSignChain{
		Method:       method,
		CanonicalURL: endpoint,
		SignBody:     signBody,
	})

	if err != nil {
		return nil, err
	}

	options = object.MergeHashMap(&object.HashMap{
		"headers": &object.HashMap{
			"Authorization": authorization,
		},
		"body": signBody,
	}, options)

	// to be setup middleware here
	//client.PushMiddleware(client.logMiddleware(), "access_token")

	// http client request
	returnResponse, err := client.HttpHelper.Df().
		Url(endpoint).Method(method).Json(options).Request()

	// decode response body to outBody
	err = client.HttpHelper.ParseResponseBodyContent(returnResponse, outBody)

	if err != nil {
		return nil, err
	}
	// decode response header to outHeader
	//headerData, _ := ioutil.ReadAll(response.Header)
	//response.Header = ioutil.NopCloser(bytes.NewBuffer(headerData))
	//err = object.JsonDecode(headerData, outHeader)

	return returnResponse, err
	//if returnRaw {
	//	return returnResponse, nil
	//} else {
	//	var rs http.Response = http.Response{
	//		StatusCode: 200,
	//		Header:     nil,
	//	}
	//	rs.Body = returnResponse.GetBody()
	//	result, _ := client.CastResponseToType(&rs, response2.TYPE_RAW)
	//	return result, nil
	//}

}

func (client *BaseClient) RequestV2(endpoint string, params *object.HashMap, method string, option *object.HashMap,
	returnRaw bool, outHeader interface{}, outBody interface{},
) (response interface{}, err error) {

	//config := (*client.App).GetConfig()

	base := &object.HashMap{
		// 微信的接口如果传入接口以外的参数，签名会失败所以这里需要区分对待参数
		"nonce_str": object.RandStringBytesMask(32),
		//"mch_id":     config.GetString("mch_id", ""),
		//"sub_mch_id": config.GetString("sub_mch_id", ""),
		//"sub_appid":  config.GetString("sub_appid", ""),
	}
	params = object.MergeHashMap(params, base)
	params = object.FilterEmptyHashMap(params)

	options, err := client.AuthSignRequestV2(endpoint, method, params, option)
	if err != nil {
		return nil, err
	}

	// http client request
	df := client.HttpHelper.Df().
		Uri(endpoint).Method(method)

	// 检查是否需要有请求参数配置
	if options != nil {
		// set query key values
		if (*options)["query"] != nil {
			queries := (*options)["query"].(*object.StringMap)
			if queries != nil {
				for k, v := range *queries {
					df.Query(k, v)
				}
			}
		}
		config := (*client.App).GetConfig()
		// 微信如果需要传debug模式
		debug := config.GetBool("debug", false)
		if debug {
			df.Query("debug", "1")
		}

		// set body json
		if (*options)["body"] != nil {
			df.Json((*options)["body"])
		}
	}

	returnResponse, err := df.Request()

	// decode response body to outBody
	err = client.HttpHelper.ParseResponseBodyContent(returnResponse, outBody)

	if err != nil {
		return nil, err
	}
	// decode response header to outHeader
	//headerData, _ := ioutil.ReadAll(response.Header)
	//response.Header = ioutil.NopCloser(bytes.NewBuffer(headerData))
	//err = object.JsonDecode(headerData, outHeader)

	return returnResponse, err

	//if returnRaw {
	//	return returnResponse, nil
	//} else {
	//	var rs http.Response = http.Response{
	//		StatusCode: 200,
	//		Header:     nil,
	//	}
	//	rs.Body = returnResponse.GetBody()
	//	result, _ := client.CastResponseToType(&rs, response2.TYPE_MAP)
	//	return result, nil
	//}

}

func (client *BaseClient) Request(endpoint string, params *object.StringMap, method string, options *object.HashMap,
	returnRaw bool, outHeader interface{}, outBody interface{},
) (response interface{}, err error) {

	config := (*client.App).GetConfig()

	// 签名访问的URL，请确保url后面不要跟参数，因为签名的参数，不包含?参数
	// 比如需要在请求的时候，把debug=false，这样url后面就不会多出 "?debug=true"
	options, err = client.AuthSignRequest(config, endpoint, method, params, options)
	if err != nil {
		return nil, err
	}
	// to be setup middleware here
	//client.PushMiddleware(client.logMiddleware(), "access_token")

	// http client request
	returnResponse, err := client.HttpHelper.Df().
		Url(endpoint).Method(method).Json(options).Request()

	// decode response body to outBody
	err = client.HttpHelper.ParseResponseBodyContent(returnResponse, outBody)

	if err != nil {
		return nil, err
	}
	// decode response header to outHeader
	//headerData, _ := ioutil.ReadAll(response.Header)
	//response.Header = ioutil.NopCloser(bytes.NewBuffer(headerData))
	//err = object.JsonDecode(headerData, outHeader)

	return returnResponse, err

	//if returnRaw {
	//	return returnResponse, nil
	//} else {
	//	var rs http.Response = http.Response{
	//		StatusCode: 200,
	//		Header:     nil,
	//	}
	//	rs.Body = returnResponse.GetBody()
	//	result, _ := client.CastResponseToType(&rs, response2.TYPE_MAP)
	//	return result, nil
	//}

}

func (client *BaseClient) RequestRaw(url string, params *object.StringMap, method string, options *object.HashMap, outHeader interface{}, outBody interface{}) (interface{}, error) {
	return client.Request(url, params, method, options, true, outHeader, outBody)
}

func (client *BaseClient) StreamDownload(requestDownload *power.RequestDownload, filePath string) (int64, error) {
	fileHandler, err := os.Create(filePath)
	if err != nil {
		return 0, err
	}
	defer fileHandler.Close()

	config := (*client.App).GetConfig()

	method := "GET"
	options, err := client.AuthSignRequest(config, requestDownload.DownloadURL, method, nil, nil)
	if err != nil {
		return 0, err
	}

	rs, err := client.HttpHelper.Df().Url(requestDownload.DownloadURL).Method(method).Json(options).Request()
	if err != nil {
		return 0, err
	}
	result, err := fileHandler.ReadFrom(rs.Body)
	if err != nil {
		return result, err
	}
	fmt2.Dump("http stream download file size:", result)

	// 校验已下载文件
	downloadedHandler, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer downloadedHandler.Close()

	fileMd5 := sha1.New()
	totalSize, err := io.Copy(fileMd5, downloadedHandler)
	if err != nil {
		return 0, err
	}

	//fmt2.Dump(totalSize)

	if requestDownload.HashValue != "" {
		fmt2.Dump(fileMd5.Sum(nil), requestDownload.HashValue)
		if fmt.Sprintf("%x", fileMd5.Sum(nil)) != requestDownload.HashValue {
			return 0, errors.New("文件损坏")
		} else {
			log.Println("文件SHA-256校验成功")
		}
	}

	return totalSize, err
}

func (client *BaseClient) RequestArray(url string, method string, options *object.HashMap, outHeader interface{}, outBody interface{}) (*object.HashMap, error) {
	returnResponse, err := client.RequestRaw(url, nil, method, options, outHeader, outBody)
	if err != nil {
		return nil, err
	}
	result, err := client.CastResponseToType(returnResponse.(*http.Response), response2.TYPE_RAW)

	return result.(*object.HashMap), err
}

func (client *BaseClient) SafeRequest(url string, params *object.HashMap, method string, option *object.HashMap, outHeader interface{}, outBody interface{}) (interface{}, error) {
	config := (*client.App).GetConfig()

	httpConfig := client.HttpHelper.GetClient().GetConfig()
	httpConfig.Cert.CertFile = config.GetString("cert_path", "")
	httpConfig.Cert.KeyFile = config.GetString("key_path", "")
	client.HttpHelper.GetClient().SetConfig(&httpConfig)

	strOutBody := ""
	// get xml string result from return raw as true
	_, err := client.RequestV2(
		url,
		params,
		method,
		option,
		true,
		outHeader,
		&strOutBody,
	)

	if err != nil {
		return nil, err
	}

	// get out result
	err = xml.Unmarshal([]byte(strOutBody), outBody)

	return outBody, err
}

func (client *BaseClient) Wrap(endpoint string) string {
	if (*client.App).InSandbox() {
		return "sandboxnew/" + endpoint
	} else {
		return endpoint
	}
}

func (client *BaseClient) AuthSignRequest(config *kernel.Config, endpoint string, method string, params *object.StringMap, options *object.HashMap) (*object.HashMap, error) {

	var err error

	base := &object.HashMap{
		"appid": config.GetString("app_id", ""),
		"mchid": config.GetString("mch_id", ""),
	}

	// init options
	if options == nil {
		options = &object.HashMap{}
	}

	// init query parameters into body
	if params != nil {
		endpoint += "?" + object.GetJoinedWithKSort(params)
		(*options)["query"] = params
	} else {
		(*options)["query"] = nil
	}

	options = object.MergeHashMap(base, client.prepends(), options)
	options = object.FilterEmptyHashMap(options)

	// check need sign body or not
	signBody := ""
	if "get" != object.Lower(method) {
		signBody, err = object.JsonEncode(options)
		if err != nil {
			return nil, err
		}
	}

	authorization, err := client.Signer.GenerateRequestSign(&support.RequestSignChain{
		Method:       method,
		CanonicalURL: endpoint,
		SignBody:     signBody,
	})

	if err != nil {
		return nil, err
	}

	options = object.MergeHashMap(&object.HashMap{
		"headers": &object.HashMap{
			"Authorization":    authorization,
			"Wechatpay-Serial": config.GetString("serial_no", ""),
		},
		"body": signBody,
	}, options)

	return options, err
}

func (client *BaseClient) AuthSignRequestV2(endpoint string, method string, params *object.HashMap, options *object.HashMap) (*object.HashMap, error) {

	var err error

	secretKey, err := (*client.App).GetKey(endpoint)
	if err != nil {
		return nil, err
	}

	strMapParams, err := object.HashMapToStringMap(params)
	if err != nil {
		return nil, err
	}

	// convert StringMap to Power StringMap
	powerStrMapParams, err := power.StringMapToPower(strMapParams)
	if err != nil {
		return nil, err
	}

	// generate md5 signature with power StringMap
	(*powerStrMapParams)["sign"] = support.GenerateSignMD5(powerStrMapParams, secretKey)

	// convert signature to xml content
	var signBody = ""
	if "get" != object.Lower(method) {
		// check need sign body or not
		objPara, err := power.PowerStringMapToObjectStringMap(powerStrMapParams)
		if err != nil {
			return nil, err
		}
		signBody = object.StringMap2Xml(objPara)
	}

	// set body content
	options = object.MergeHashMap(&object.HashMap{
		"body": signBody,
	}, options)

	return options, err
}

// ----------------------------------------------------------------------
func (client *BaseClient) OverrideGetMiddlewares() {
	client.OverrideGetMiddlewareOfAccessToken()
	client.OverrideGetMiddlewareOfLog()
	client.OverrideGetMiddlewareOfRefreshAccessToken()
}

func (client *BaseClient) OverrideGetMiddlewareOfAccessToken() {
	client.GetMiddlewareOfAccessToken = func(handle contract.RequestHandle) contract.RequestHandle {
		return func(request *http.Request) (response *http.Response, err error) {
			// 前置中间件
			//fmt.Println("获取access token, 在请求前执行")

			accessToken := (*client.App).GetAccessToken()

			if accessToken != nil {
				config := (*client.App).GetContainer().Config
				_, err = accessToken.ApplyToRequest(request, config)
			}

			if err != nil {
				return nil, err
			}

			response, err = handle(request)
			// handle 执行之后就可以操作 response 和 err

			// 后置中间件
			//fmt.Println("获取access token, 在请求后执行")
			return
		}
	}
}
