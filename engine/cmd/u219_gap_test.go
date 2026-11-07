package main
import("net/http";"net/http/httptest";"testing")
func TestU219PrivateIntentCanReachBoundHandler(t *testing.T){
 t.Setenv("ADAM_MAIN_PLAYBACK","1");received:=false
 handler:=mainPlaybackBoundary(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){received=true;w.WriteHeader(202)}))
 w:=httptest.NewRecorder();handler.ServeHTTP(w,httptest.NewRequest("POST","https://127.0.0.1/api/v1/main/auth",nil))
 if !received||w.Code!=202{t.Fatalf("private intent never reaches an Engine handler: received=%v status=%d",received,w.Code)}
}
