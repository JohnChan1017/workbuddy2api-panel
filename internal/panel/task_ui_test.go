package panel

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTaskUIReportsVerifiedStateAndIgnoresStaleAccounts(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	harness := `const fs = require('fs'), vm = require('vm'), assert = require('assert');
const inert = new Proxy(function(){}, {get(t,k){if(k===Symbol.toPrimitive)return()=>'';return inert;},set(){return true;},apply(){return inert;}});
const elements = new Map();
function element(id) {
  if (!elements.has(id)) elements.set(id, new Proxy({innerHTML:'',textContent:'',hidden:false,disabled:false,classList:inert}, {get(t,k){return k in t?t[k]:inert;},set(t,k,v){t[k]=v;return true;}}));
  return elements.get(id);
}
const sandbox = {
  location:{hash:'#accounts'},history:{replaceState(){}},localStorage:{getItem:()=>null,setItem(){}},
  navigator:{clipboard:inert},document:{querySelectorAll:()=>[],querySelector:()=>inert,getElementById:element,addEventListener(){},documentElement:inert,head:inert,body:inert,createElement:()=>inert,cookie:''},
  fetch:()=>new Promise(()=>{}),addEventListener(){},removeEventListener(){},matchMedia:()=>({matches:false,addEventListener(){}}),
  setInterval:()=>0,clearInterval(){},setTimeout:()=>0,clearTimeout(){},console,URL,assert
};
sandbox.window=sandbox;
vm.createContext(sandbox);
vm.runInContext(fs.readFileSync(process.argv[2],'utf8'),sandbox);
vm.runInContext(` + "`" + `(async()=>{
  const notices=[]; toast=(message,kind)=>notices.push({message,kind});
  showTaskAcceptance({ok:true,accepted:0,verified:false,message:'状态未确认'});
  assert.equal(notices.at(-1).kind,'err');
  showTaskAcceptance({ok:true,accepted:1,verified:true,message:'已确认接受'});
  assert.equal(notices.at(-1).kind,'ok');
  showTaskAcceptance({ok:true});
  assert.equal(notices.at(-1).kind,'err'); // An old response must not claim verified success.

  taskUID='personal';
  api=async()=>({enterprise_account:false,automation_block_reason:'个人账号；任务兼容性未确认',tasks:[
    {task_code:'chat_5',accept_status:'in_progress',current:1,target:5},
    {task_code:'new-task',accept_status:'not_accepted',current:0,target:1},
    {task_code:'done-task',accept_status:'completed',current:1,target:1}
  ]});
  await loadTasks();
  assert.equal($('btnTaskAutoAll').disabled,true);
  assert.equal($('taskAutomationNotice').hidden,false);
  assert.match($('taskBody').innerHTML,/进行中/);
  assert.match($('taskBody').innerHTML,/已完成/);
  assert.doesNotMatch($('taskBody').innerHTML,/data-t="auto"/);
  assert.equal(($('taskBody').innerHTML.match(/data-t="accept"/g)||[]).length,1);

  // The older account's response must not replace the newly opened account.
  let releaseOld;
  api=()=>new Promise(resolve=>{releaseOld=resolve;});
  taskUID='old-account'; const oldLoad=loadTasks();
  taskUID='new-account';
  api=async()=>({automation_block_reason:'new-account-notice',tasks:[]});
  await loadTasks();
  releaseOld({automation_block_reason:'stale-notice',tasks:[]});
  await oldLoad;
  assert.equal($('taskAutomationNotice').textContent,'new-account-notice');
})()` + "`" + `,sandbox).then(()=>process.exit(0),e=>{console.error(e);process.exit(1);});
`
	path := filepath.Join(t.TempDir(), "task-ui.cjs")
	if err := os.WriteFile(path, []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, path, "app.js").CombinedOutput(); err != nil {
		t.Fatalf("task UI regression: %v\n%s", err, out)
	}
}
