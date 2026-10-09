package task
import("github.com/v03413/tronprotocol/api";"context";"encoding/json";"os";"testing";"time")
func TestAuthorizedHTTPSObserverReadOnlyCapability(t *testing.T){
 if os.Getenv("BEPUSDT_TRON_HTTPS_CONFIG_FILE")==""{t.Skip("live authorized file reference absent")}
 c,e:=newTronHTTPSClient();if e!=nil{t.Fatal(e)};defer c.close()
 ctx,cancel:=context.WithTimeout(context.Background(),25*time.Second);defer cancel()
 head,e:=c.GetNowBlock2(ctx,nil);if e!=nil{t.Fatal(e)}
 number:=head.GetBlockHeader().GetRawData().GetNumber()
 block,e:=c.GetBlockByNum2(ctx,&api.NumberMessage{Num:number});if e!=nil{t.Fatal(e)}
 millis:=block.GetBlockHeader().GetRawData().GetTimestamp()
 if millis!=head.GetBlockHeader().GetRawData().GetTimestamp(){t.Fatal("canonical head block milliseconds differ")}
 var index struct{Success bool `json:"success"`;Data []json.RawMessage `json:"data"`}
 if e=c.read(ctx,"GET","/v1/accounts/"+httpsObserverRecipient+"/transactions/trc20?only_to=true&only_confirmed=true&contract_address="+httpsObserverContract+"&limit=20",nil,&index);e!=nil{t.Fatal(e)}
 if !index.Success{t.Fatal("index capability unavailable")}
 t.Logf("READ_ONLY_PROVIDER_A_HINT_CAPABILITY_PASS height=%d block_timestamp_ms=%d index_rows=%d credit_authorized=false",number,millis,len(index.Data))
}
