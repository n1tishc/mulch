import { test,expect } from "@playwright/test";

test("task, follow-up, stop, reload, rename and commands",async({page,request})=>{
  const errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));
  await page.goto("/");await expect(page.getByLabel("Working directory")).toHaveValue(/mulch-web-fixture/);
  await page.getByLabel("Message",{exact:true}).fill("Write the fixture result.");await page.getByRole("button",{name:"Send message"}).click();
  await expect(page.locator(".task-end")).toContainText("completed");await expect(page.getByText("Implemented the fixture change.")).toHaveCount(1);await expect(page.locator(".status-notice")).toHaveText("Task completed.");
  await page.locator(".tool-call summary").click();await expect(page.locator(".tool-content")).toContainText("result.txt");
  const url=page.url(),id=new URL(url).searchParams.get("session")!;
  await expect(page.getByRole("button",{name:"Copy code"})).toBeVisible();
  await page.getByLabel("Message",{exact:true}).fill("Continue with a follow-up.");await expect(page.getByRole("button",{name:"Send message"})).toBeEnabled();await page.getByRole("button",{name:"Send message"}).click();
  await expect(page.locator(".task-end")).toHaveCount(2);expect(new URL(page.url()).searchParams.get("session")).toBe(id);
  await page.getByLabel("Message",{exact:true}).fill("slow task to cancel");await expect(page.getByRole("button",{name:"Send message"})).toBeEnabled();await page.getByRole("button",{name:"Send message"}).click();
  await expect(page.getByRole("button",{name:"Stop task"})).toBeEnabled();await page.reload();await page.getByRole("button",{name:"Stop task"}).click();await expect(page.locator(".task-end").last()).toContainText("cancelled");
  await page.getByLabel("Message",{exact:true}).fill("Continue after cancellation.");await expect(page.getByRole("button",{name:"Send message"})).toBeEnabled();await page.getByRole("button",{name:"Send message"}).click();await expect(page.locator(".task-end")).toHaveCount(4);await expect(page.locator(".task-end").last()).toContainText("completed");await expect(page.locator(".session-heading p")).toContainText("completed");await expect(page.locator(".working")).toHaveCount(0);await expect(page.locator(".session-row.active small")).toContainText("completed");await page.screenshot({path:"test-results/desktop-conversation.png",fullPage:true});
  await page.getByLabel("Message",{exact:true}).fill("A saved draft");await page.reload();await expect(page.getByLabel("Message",{exact:true})).toHaveValue("A saved draft");await expect(page.locator(".status-notice")).not.toContainText("Task completed.");
  await page.getByRole("button",{name:"Rename",exact:true}).click();await page.getByLabel("Session name").fill("Browser acceptance");await page.getByRole("button",{name:"Save name"}).click();await expect(page.getByRole("heading",{name:"Browser acceptance"})).toBeVisible();
  await page.keyboard.press("Control+k");await page.getByLabel("Find a command").fill("Show repairs");await page.keyboard.press("Enter");await expect(page.getByRole("heading",{name:"Repair decisions"})).toBeVisible();
  await page.getByRole("button",{name:"Close inspector",exact:true}).click();await page.getByRole("button",{name:"New conversation",exact:true}).click();await expect(page.getByRole("heading",{name:"What are we working on?"})).toBeVisible();
  const events=await (await request.get(`/api/sessions/${id}/events`)).json();expect(events.filter((e:{type:string})=>e.type==="user.message")).toHaveLength(4);expect(errors).toEqual([]);
});

test("lost response retry preserves submission identity and draft",async({page,request})=>{
  await page.goto("/");let intercepted=false;
  await page.route("**/api/sessions",async route=>{if(route.request().method()==="POST"&&!intercepted){intercepted=true;await route.fetch();await route.abort("failed");}else await route.continue();});
  await page.getByLabel("Message",{exact:true}).fill("Lost response fixture");await page.getByRole("button",{name:"Send message"}).click();await expect(page.getByRole("alert")).toContainText("draft is saved");
  await page.reload();await expect(page.getByLabel("Message",{exact:true})).toHaveValue("Lost response fixture");await page.getByRole("button",{name:"Send message"}).click();await expect(page.locator(".task-end")).toContainText("completed");
  const sessions=await(await request.get("/api/sessions")).json();expect(sessions.filter((s:{Task:string})=>s.Task==="Lost response fixture")).toHaveLength(1);
});

test("repair occurrences, policy, candidate ties and ungraded outcome",async({page})=>{
  await page.goto("/?session=evidence");await page.getByRole("button",{name:"Inspect",exact:true}).click();
  await expect(page.locator(".dimension").filter({hasText:"coherence"})).toContainText("40.0 · reused");await expect(page.locator(".facts")).toContainText("80");await expect(page.locator(".dimension").filter({hasText:"staleness"}).locator("meter")).toHaveCount(0);
  await page.getByRole("tab",{name:"repairs",exact:true}).click();await page.locator(".decision").filter({hasText:"Synthetic confirmed low relevance"}).click();await expect(page.getByText("#4 · visible → hidden",{exact:true})).toBeVisible();await expect(page.getByRole("region",{name:"Selected repair evidence"})).toBeFocused();
  await page.locator(".selected-evidence summary").click();await expect(page.locator(".selected-evidence pre")).toContainText("Identical payload");
  await page.locator(".decision").filter({hasText:"race.end"}).click();await expect(page.locator(".candidate")).toHaveCount(2);await expect(page.locator(".candidate").first()).toContainText("Selected");await expect(page.getByText("Not independently graded",{exact:true})).toBeVisible();
  await page.screenshot({path:"test-results/desktop-evidence.png",fullPage:true});
});

test("external ownership, missing config, invalid workspace and mobile focus",async({page})=>{
  await page.goto("/?session=external");await expect(page.getByRole("button",{name:"Stop task"})).toBeDisabled();await expect(page.getByText("Running in another process. Control it from its terminal.")).toBeVisible();
  await page.goto("/");await page.getByLabel("Working directory").fill("/missing/mulch-workspace");await page.getByLabel("Message",{exact:true}).fill("Do not lose this prompt");await page.getByRole("button",{name:"Send message"}).click();await expect(page.getByRole("alert")).toContainText("no such file");await expect(page.getByLabel("Message",{exact:true})).toHaveValue("Do not lose this prompt");
  await page.setViewportSize({width:390,height:844});await page.goto("/?session=evidence");await page.getByRole("button",{name:"Open sessions"}).click();await expect(page.getByRole("dialog",{name:"Conversations"})).toBeVisible();await page.keyboard.press("Escape");await expect(page.getByRole("button",{name:"Open sessions"})).toBeFocused();
  await expect(page.locator(".task-end")).toBeVisible();await page.screenshot({path:"test-results/mobile-conversation.png",fullPage:true});await page.getByRole("button",{name:"Inspect",exact:true}).click();await expect(page.getByRole("dialog",{name:"Context inspector"})).toBeVisible();await page.screenshot({path:"test-results/mobile-inspector.png",fullPage:true});await page.keyboard.press("Escape");await expect(page.getByRole("button",{name:"Inspect",exact:true})).toBeFocused();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  await page.route("**/api/config",route=>route.fulfill({json:{ready:false,workspace:"/tmp",model:"none",mode:"plain"}}));await page.goto("/");await expect(page.getByText("Connect a model to start coding")).toBeVisible();await page.getByLabel("Message",{exact:true}).fill("read only");await expect(page.getByRole("button",{name:"Send message"})).toBeDisabled();
});

test("10,000 event replay, reconnect and desktop welcome",async({page})=>{
	await page.addInitScript(()=>{const Native=window.WebSocket;(window as any).__sockets=[];window.WebSocket=class extends Native{constructor(url:string|URL,protocols?:string|string[]){super(url,protocols);(window as any).__sockets.push(this);}};});
  await page.goto("/");await expect(page.getByLabel("Working directory")).toHaveValue(/mulch-web-fixture/);await page.screenshot({path:"test-results/desktop-welcome.png",fullPage:true});
  await page.goto("/?session=load");await expect(page.locator(".message.assistant")).toHaveCount(1);await expect(page.locator(".prose")).toContainText("x".repeat(10000));
  const latency=await page.getByLabel("Message",{exact:true}).evaluate((el:HTMLTextAreaElement)=>new Promise<number>(resolve=>{const start=performance.now();el.value="responsive";el.dispatchEvent(new Event("input",{bubbles:true}));requestAnimationFrame(()=>resolve(performance.now()-start));}));expect(latency).toBeLessThan(100);
  await expect(page.locator(".connection")).toContainText("Connected");await page.evaluate(()=>(window as any).__sockets.at(-1).close());await expect.poll(()=>page.evaluate(()=>(window as any).__sockets.length)).toBeGreaterThan(1);await expect(page.locator(".connection")).toContainText("Connected");await expect(page.locator(".message.assistant")).toHaveCount(1);
});

test("provider failure recovers and Markdown cannot execute HTML",async({page})=>{
  await page.goto("/");await page.getByLabel("Message",{exact:true}).fill("provider failure");await page.getByRole("button",{name:"Send message"}).click();await expect(page.locator(".task-end")).toContainText("failed");await expect(page.locator(".status-notice")).toHaveText("Task failed.");
  await page.getByLabel("Message",{exact:true}).fill('<img src=x onerror="window.__xss=1"><script>window.__xss=1</script>\n[safe?](javascript:alert(1))');await expect(page.getByRole("button",{name:"Send message"})).toBeEnabled();await page.getByRole("button",{name:"Send message"}).click();await expect(page.locator(".task-end")).toHaveCount(2);await expect(page.locator(".task-end").last()).toContainText("completed");await expect(page.locator(".prose img,.prose script,.prose a[href^='javascript:']")).toHaveCount(0);expect(await page.evaluate(()=>(window as any).__xss)).toBeUndefined();
});

test("closing the browser leaves backend work running",async({page,request})=>{
  await page.goto("/");await page.getByLabel("Message",{exact:true}).fill("slow task survives browser closure");await page.getByRole("button",{name:"Send message"}).click();await expect(page.getByRole("button",{name:"Stop task"})).toBeEnabled();const id=new URL(page.url()).searchParams.get("session")!;
  await page.close();const state=await(await request.get(`/api/sessions/${id}`)).json();expect(state.owner).toBe("daemon");expect(state.session.Status).toBe("running");await request.delete(`/api/sessions/${id}`);await expect.poll(async()=>{const d=await(await request.get(`/api/sessions/${id}`)).json();return d.session.Status;}).toBe("cancelled");
});
