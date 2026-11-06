// SOURCE_ONLY: definitions for the future full-round batch, not executed.
import test from 'node:test';
import assert from 'node:assert/strict';
import { ControlClient, RefundFlow, refundApplicationOf, refundReviewLabel, refundFinancialLabel, type RefundApplication, type Session, type Order, type Storage, type Request, type Response, type Transport } from '../src/index';

const session: Session = { session_id:'ses_refund',client_type:'WEB',expires_at:'2099-01-01T00:00:00Z',csrf_token:'synthetic_csrf',profile:{user_id:'usr_buyer',roles:['BUYER'],permission_scope_ids:['perm_refund_create','perm_refund_read'],identity_state:'ACTIVE',version:1} };
const order: Order = { order_id:'ord_original',buyer_id:'usr_buyer',seller_id:'usr_seller',version:1,state:'READY',contract:{},created_at:'2026-11-06T00:00:00Z',updated_at:'2026-11-06T00:00:00Z' };
const application = (patch: Partial<RefundApplication> = {}): RefundApplication => ({refund_id:'refund_original',order_id:order.order_id,payment_id:'pay_original',version:1,state:'REQUESTED',requested_fen:100,confirmed_fen:0,updated_at:'2026-11-06T00:00:00Z',review_state:'PENDING',review_version:1,reason_code:'NO_LONGER_NEEDED',review_required:false,...patch});
const ok = (data: unknown): Response => ({status:200,headers:{},data});
function storage(){const map=new Map<string,string>();const port:Storage={get:k=>map.get(k)??null,set:(k,v)=>{map.set(k,v);},remove:k=>{map.delete(k);}};return {map,port};}
function fixture(transport:Transport,store=storage()){
 const client=new ControlClient('',transport,'WEB');client.setSession(session);const flow=new RefundFlow(client,store.port,async n=>new Uint8Array(n).fill(7));flow.select(order);return {client,flow,store};
}
test('refund review rejection never claims channel failure or refunded money',()=>{
 const v=application({review_state:'REJECTED',review_version:2,version:2,review_reason_code:'INSUFFICIENT_EVIDENCE'});
 assert.match(refundReviewLabel(v),/未获批准/);assert.equal(refundFinancialLabel(v),'未创建渠道退款');assert.equal(refundApplicationOf(v).confirmed_fen,0);
 assert.throws(()=>refundApplicationOf({...v,state:'SUCCEEDED',confirmed_fen:100}));
});
test('refund parser rejects foreign fields and unconfirmed success amounts',()=>{
 for(const v of [{...application(),prompt:'private'},{...application(),confirmed_fen:1},{...application(),requested_fen:5001},{...application(),review_state:'APPROVED',state:'SUCCEEDED',confirmed_fen:99},{...application(),state:['SUCCEEDED']},{...application(),reason_code:['NO_LONGER_NEEDED']}])assert.throws(()=>refundApplicationOf(v));
});
test('unknown refund submission survives restart with the exact original key and body',async()=>{
 const requests:Request[]=[];let first=true;const transport:Transport=async r=>{requests.push(r);if(first){first=false;throw new Error('lost response');}return ok(application());};
 const a=fixture(transport);await a.flow.apply('NO_LONGER_NEEDED',100);assert.equal(a.flow.snapshot().pending,true);a.flow.dispose();
 const b=fixture(transport,a.store);await b.flow.recover();assert.equal(requests.length,2);assert.equal(requests[0].headers['Idempotency-Key'],requests[1].headers['Idempotency-Key']);assert.equal(requests[0].body,requests[1].body);assert.equal(b.store.map.size,0);b.flow.dispose();
});
test('new refund parameters cannot replace an unresolved original request',async()=>{
 let calls=0;const f=fixture(async()=>{calls++;throw new Error('lost');});await f.flow.apply('NO_LONGER_NEEDED',100);await f.flow.apply('SERVICE_UNAVAILABLE',101);assert.equal(calls,1);assert.equal(f.flow.snapshot().pending,true);f.flow.dispose();
});
test('late refund response cannot repopulate a different account or erase its original command',async()=>{
 let resolve!:(r:Response)=>void;const f=fixture(async()=>new Promise<Response>(r=>{resolve=r;}));const work=f.flow.apply('NO_LONGER_NEEDED',100);
 await new Promise(r=>setImmediate(r));f.client.setSession({...session,profile:{...session.profile,user_id:'usr_other'}});resolve(ok(application()));await work;
 assert.equal(f.flow.snapshot().items.length,0);assert.equal(f.flow.snapshot().order,'');assert.ok(f.store.port.get('refund-pending:usr_buyer'));f.flow.dispose();
});
test('refund journal holds only bounded control metadata and no channel credential',async()=>{
 const f=fixture(async()=>{throw new Error('lost');});await f.flow.apply('NO_LONGER_NEEDED',100);const raw=[...f.store.map.values()].join('');
 for(const forbidden of ['synthetic_csrf','Payment-Proof','payment_proof','client_session','prompt','data:image'])assert.ok(!raw.includes(forbidden));assert.match(raw,/requested_fen/);f.flow.dispose();
});
test('corrupt refund journal is retained and prevents new financial commands',async()=>{
 let calls=0;const f=fixture(async()=>{calls++;return ok(application());});f.store.port.set('refund-pending:usr_buyer','{"profile":"TSM_REFUND_COMMAND_1","prompt":"private"}');await f.flow.apply('NO_LONGER_NEEDED',100);assert.equal(calls,0);assert.ok(f.store.port.get('refund-pending:usr_buyer'));assert.equal(f.flow.snapshot().pending,true);f.flow.dispose();
});
test('review revocation preserves successful original-channel financial facts',()=>{
 const v=refundApplicationOf(application({review_state:'REVOKED',review_version:3,version:6,state:'SUCCEEDED',confirmed_fen:100,review_reason_code:'AUTHORITY_REVOKED'}));assert.match(refundReviewLabel(v),/撤销/);assert.match(refundFinancialLabel(v),/确认退款成功/);
});
test('an order switch ignores a late original-order refund page',async()=>{
 let resolve!:(r:Response)=>void;const f=fixture(async()=>new Promise<Response>(r=>{resolve=r;}));const work=f.flow.refresh();f.flow.select({...order,order_id:'ord_another'});resolve(ok({items:[application()],next_cursor:null}));await work;assert.equal(f.flow.snapshot().order,'ord_another');assert.equal(f.flow.snapshot().items.length,0);f.flow.dispose();
});
test('review decisions use current original versions and explicit registered route',async()=>{
 const requests:Request[]=[];const f=fixture(async r=>{requests.push(r);return r.method==='GET'?ok({...application(),buyer_id:'usr_original_buyer',seller_id:'usr_seller',order_version:4,payment_version:6,evidence_refs:[],refund_route_refs:['rroute_original'],collected_fen:500,held_refund_fen:0,maximum_refundable_fen:500}):ok(application({review_state:'APPROVED',review_version:2,version:2}));});
 await f.flow.openReview('refund_original');await f.flow.decide(true,'rroute_original',500);const body=JSON.parse(requests[1].body!);assert.equal(body.expected_order_version,4);assert.equal(body.expected_payment_version,6);assert.equal(body.refund_route_ref,'rroute_original');assert.equal(body.maximum_refundable_fen,500);f.flow.dispose();
});
