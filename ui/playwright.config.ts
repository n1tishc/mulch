import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir:"./e2e",fullyParallel:false,workers:1,timeout:30000,
  use:{baseURL:"http://127.0.0.1:4144",viewport:{width:1440,height:1000},trace:"retain-on-failure"},
  webServer:{command:"cd .. && go run ./ui/testserver",url:"http://127.0.0.1:4144/api/config",timeout:120000,reuseExistingServer:false,gracefulShutdown:{signal:"SIGINT",timeout:5000}},
});
