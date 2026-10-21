// Only injected into the HTML served by browser.test.cjs. No production test API.
module.exports = `
window.__test = {
  load(mode = 'expanded') {
    resetData(); GROUPS.forEach(g => D[g].clear()); PN.clear(); opened.clear(); collapsed.clear();
    mergeDict({clients:[{id:1,kind:'sdk',label:'Demo client'},{id:2,kind:'sdk',label:'Sessionless client'}],
      sessions:[{id:1,short:'root',sid:'demo-root',client:1}, ...Array.from({length:8},(_,i)=>({id:i+2,short:'agent-'+i,sid:'demo-agent-'+i,parent:1,client:1}))],
      rules:[{id:1,alias:'demo-model',model:'demo-model'}], exits:[{id:1,auth_index:'demo',provider:'demo',label:'demo credential'}]});
    netState='ok'; setPill();
    const ts = now();
    const add = (id,session,client=1) => putRec({id,client,session,rule:1,exit:1,state:'done',t0:ts-10000,t1:ts-5000,lat:5000,status:200,tok:{in:100,out:50,total:150}},true);
    add('root',mode==='none'?0:1); add('sessionless',0,2);
    if(mode==='children'||mode==='folded') for(let i=0;i<(mode==='folded'?8:2);i++) add('child-'+i,i+2);
    if(mode==='collapsed') collapsed.add(1); else opened.add(1);
    computeFlow(ts); kick();
  },
  state(state) {
    const prev = recs.get('flight');
    putRec({id:'flight',client:1,session:1,rule:1,...(state==='queued'?{}:{exit:1}),state,t0:prev?prev.t0:now(),...(state==='done'||state==='failed'?{t1:now(),status:state==='done'?200:500}:{})},false);
    computeFlow(now()); kick();
    return {...flights.get('flight')};
  },
  inspect() {
    computeFlow(now());
    const paths = [...recs.values()].map(r=>({id:r.id,keys:pathKeys(r),path:buildPath(r)}));
    return {direction:flowDirection,layout:effLayout(),view,rangeMs,viewEnd,paused,records:recs.size,
      nodes:[...F.nodes.values()].map(n=>({key:n.key,type:n.type,x:n.x,y:n.y,w:n.w,h:n.h,fold:n.fold,hit:hitFlow(n.type==='r'?hub.x+hub.w/2:n.x+n.w/2,n.y)?.k})),
      hub:{...hub},W,FH,paths,edges:[...F.edges.values()].map(e=>({key:e.k,g:edgeG(e)})),flights:[...flights.values()]};
  },
  pinTimeline() {viewEnd=now()-60000; rangeMs=30000; LSset('lr-range',rangeMs); kick();},
  settled() {return [...F.nodes.values()].every(n=>Math.abs(n.y-n.ty)<.5);},
  drawLabels() {const labels=[], previous=X; X=document.createElement('canvas').getContext('2d'); X.fillText=function(text,x,y){labels.push({text,x,y});};try{drawChrome();}finally{X=previous;}return labels;}
};
`;
