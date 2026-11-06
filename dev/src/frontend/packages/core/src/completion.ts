import { ControlClient, ApiError, errorText } from './client';
import { object, type Order } from './contracts';
import { type Storage, type SecureBytes, newKey } from './auth';

const id = (v: unknown): v is string => typeof v === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(v);
const hash = (v: unknown): v is string => typeof v === 'string' && /^[0-9a-f]{64}$/.test(v);
const integer = (v: unknown, min = 0, max = Number.MAX_SAFE_INTEGER): v is number => typeof v === 'number' && Number.isSafeInteger(v) && v >= min && v <= max;
const exact = (v: Record<string, unknown>, keys: string[]) => { if (Object.keys(v).some(k => !keys.includes(k))) throw new ApiError('INVALID_RESPONSE'); };
const time = (v: unknown): v is string => typeof v === 'string' && v.length <= 40 && /^\d{4}-\d{2}-\d{2}T/.test(v) && Number.isFinite(Date.parse(v));
const includes = (v: unknown, values: readonly string[]): v is string => typeof v === 'string' && values.includes(v);

export interface BillingOutcome { state: 'HELD_PENDING_EVIDENCE' | 'HELD_FINANCIAL_REVIEW' | 'QUALIFIED_CHANNEL_COST_UNKNOWN'; qualification_id?: string; journal_id?: string; gross_fen: number; commission_fen: number; provisional_seller_fen: number }
export function billingOutcomeOf(value: unknown): BillingOutcome {
 const v = object(value); exact(v, ['state','qualification_id','journal_id','gross_fen','commission_fen','provisional_seller_fen']);
 if (!includes(v.state,['HELD_PENDING_EVIDENCE','HELD_FINANCIAL_REVIEW','QUALIFIED_CHANNEL_COST_UNKNOWN']) || !integer(v.gross_fen) || !integer(v.commission_fen,0,v.gross_fen) || !integer(v.provisional_seller_fen,0,v.gross_fen) || v.commission_fen + v.provisional_seller_fen !== v.gross_fen || (v.qualification_id !== undefined && !id(v.qualification_id)) || (v.journal_id !== undefined && !id(v.journal_id))) throw new ApiError('INVALID_RESPONSE');
 if (v.state === 'QUALIFIED_CHANNEL_COST_UNKNOWN' ? !id(v.qualification_id) || (v.gross_fen > 0 ? !id(v.journal_id) : v.journal_id !== undefined) : v.gross_fen !== 0 || v.qualification_id !== undefined || v.journal_id !== undefined) throw new ApiError('INVALID_RESPONSE');
 return v as unknown as BillingOutcome;
}
export interface OrderBill { order_id: string; version: number; gross_fen: number; commission_fen: number; provisional_seller_fen: number; reserved_fen: number; unverified_fen: number; channel_cost_state: 'UNKNOWN'; payout_state: 'NOT_QUALIFIED'; is_final: false; as_of: string }
export function orderBillOf(value: unknown): OrderBill {
 const v = object(value); exact(v,['order_id','version','gross_fen','commission_fen','provisional_seller_fen','reserved_fen','unverified_fen','channel_cost_state','payout_state','is_final','as_of']);
 if (!id(v.order_id) || !integer(v.version,1) || !integer(v.gross_fen) || !integer(v.commission_fen,0,v.gross_fen) || !integer(v.provisional_seller_fen,0,v.gross_fen) || v.commission_fen + v.provisional_seller_fen !== v.gross_fen || !integer(v.reserved_fen) || !integer(v.unverified_fen) || v.channel_cost_state !== 'UNKNOWN' || v.payout_state !== 'NOT_QUALIFIED' || v.is_final !== false || !time(v.as_of)) throw new ApiError('INVALID_RESPONSE');
 return v as unknown as OrderBill;
}
export interface OriginalResult { delivery_id: string; attempt_id: string; revision: number; manifest_hash: string; output_digest: string; output_bytes: number; state: string; decision_deadline: string; usage_state: 'VERIFIED' | 'UNVERIFIED' }
function resultOf(value: unknown): OriginalResult {
 const v=object(value); exact(v,['delivery_id','attempt_id','revision','manifest_hash','output_digest','output_bytes','state','decision_deadline','usage_state']);
 if (!id(v.delivery_id)||!id(v.attempt_id)||v.revision!==1||!hash(v.manifest_hash)||!hash(v.output_digest)||!integer(v.output_bytes,0,32<<20)||!includes(v.state,['REGISTERED','VALIDATING','WAITING_BUYER','VALID','INVALID','STALE'])||!time(v.decision_deadline)||!includes(v.usage_state,['VERIFIED','UNVERIFIED'])) throw new ApiError('INVALID_RESPONSE');
 return v as unknown as OriginalResult;
}
export interface OriginalJob { job_id: string; order_id: string; seller_id: string; state: string; version: number; current_attempt_id?: string; accepted_delivery_id?: string; deadline_at: string; results: OriginalResult[]; billing: BillingOutcome }
export function originalJobOf(value: unknown): OriginalJob {
 const v=object(value); exact(v,['job_id','order_id','seller_id','state','version','current_attempt_id','accepted_delivery_id','deadline_at','results','billing']);
 if (!id(v.job_id)||!id(v.order_id)||!id(v.seller_id)||!integer(v.version,1)||!includes(v.state,['INPUT_PENDING','QUEUED','RUNNING','VALIDATING','WAITING_BUYER','ACCEPTED','FAILED','CANCELED','UNVERIFIED'])||!time(v.deadline_at)||(v.current_attempt_id!==undefined&&!id(v.current_attempt_id))||(v.accepted_delivery_id!==undefined&&!id(v.accepted_delivery_id))||!Array.isArray(v.results)||v.results.length>3) throw new ApiError('INVALID_RESPONSE');
 const results=v.results.map(resultOf), billing=billingOutcomeOf(v.billing);
 if (new Set(results.map(x=>x.delivery_id)).size!==results.length || (v.state==='ACCEPTED' ? !results.some(x=>x.delivery_id===v.accepted_delivery_id&&x.attempt_id===v.current_attempt_id&&x.state==='VALID') : v.accepted_delivery_id!==undefined)) throw new ApiError('INVALID_RESPONSE');
 return {...v,results,billing} as unknown as OriginalJob;
}
interface Decision { expected_job_version: number; delivery_id: string; revision: number; manifest_hash: string; reviewed_output_digest: string; decision: 'ACCEPT' | 'REJECT' }
interface Cancel { expected_job_version: number; reason_code: 'BUYER_REQUEST' }
interface Pending { profile: 'TSM_COMPLETION_COMMAND_1'; owner: string; order: string; job: string; operation: 'decision' | 'cancel'; key: string; body: Decision | Cancel }
export interface LocalReviewReceipt { profile_id: 'P11_LOCAL_RESULT_REVIEW_1'; job_id: string; delivery_id: string; revision: number; manifest_hash: string; output_digest: string }
function pendingOf(raw: string, owner: string): Pending {
 if (raw.length>8192) throw new Error('原任务操作记录需要核对，请保留记录。');
 const v=object(JSON.parse(raw));exact(v,['profile','owner','order','job','operation','key','body']);
 if(v.profile!=='TSM_COMPLETION_COMMAND_1'||v.owner!==owner||!id(v.owner)||!id(v.order)||!id(v.job)||!includes(v.operation,['decision','cancel'])||typeof v.key!=='string'||!/^[!-~]{8,128}$/.test(v.key)) throw new Error('原任务操作记录需要核对，请保留记录。');
 const b=object(v.body);
 if(!integer(b.expected_job_version,1,2147483646)) throw new ApiError('INVALID_REQUEST');
 if(v.operation==='decision') {exact(b,['expected_job_version','delivery_id','revision','manifest_hash','reviewed_output_digest','decision']);if(!id(b.delivery_id)||b.revision!==1||!hash(b.manifest_hash)||!hash(b.reviewed_output_digest)||!includes(b.decision,['ACCEPT','REJECT']))throw new ApiError('INVALID_REQUEST');}
 else {exact(b,['expected_job_version','reason_code']);if(b.reason_code!=='BUYER_REQUEST')throw new ApiError('INVALID_REQUEST');}
 return v as unknown as Pending;
}
function ackOf(value: unknown) {
 const v=object(value);exact(v,['job_id','delivery_id','id','version','state','billing']);if(!id(v.job_id)||(v.delivery_id!==undefined&&!id(v.delivery_id))||!id(v.id)||!integer(v.version,1)||!includes(v.state,['ACCEPTED','FAILED','CANCELED']))throw new ApiError('INVALID_RESPONSE');return {...v,billing:billingOutcomeOf(v.billing)};
}
export interface CompletionState { order: string; bill: OrderBill | null; job: OriginalJob | null; busy: boolean; pending: boolean; error: string }
const initial=():CompletionState=>({order:'',bill:null,job:null,busy:false,pending:false,error:''});
export class CompletionFlow {
 private state=initial();private order:Order|null=null;private epoch=0;private listeners=new Set<()=>void>();private unsubscribe:()=>void;
 constructor(private client:ControlClient,private storage:Storage,private random:SecureBytes){this.unsubscribe=client.onSessionChange(()=>{this.epoch++;this.order=null;this.state=initial();this.emit();});}
 subscribe=(f:()=>void)=>{this.listeners.add(f);return()=>{this.listeners.delete(f);};};snapshot=()=>this.state;
 private emit(){this.listeners.forEach(f=>f());}private update(p:Partial<CompletionState>){this.state={...this.state,...p};this.emit();}
 bindOrder(order:Order|null){const owner=this.client.current()?.profile.user_id;if(order&&owner!==order.buyer_id&&owner!==order.seller_id)order=null;if(this.order?.order_id===order?.order_id)return;this.epoch++;this.order=order;this.state={...initial(),order:order?.order_id||''};this.emit();}
 private context(epoch:number,owner:string){if(epoch!==this.epoch||this.client.current()?.profile.user_id!==owner)throw new ApiError('COMPLETION_CONTEXT_CHANGED');}
 private readPending(owner:string){const raw=this.storage.get('completion-pending:'+owner);return raw?pendingOf(raw,owner):null;}
 async refresh(job?:string){if(this.state.busy||!this.order)return;const epoch=this.epoch,owner=this.client.current()?.profile.user_id;if(!owner)return;const order=this.order.order_id;this.update({busy:true,error:''});
  try{const pending=this.readPending(owner);const bill=await this.client.request('GET','/orders/'+order+'/bill',orderBillOf);this.context(epoch,owner);if(bill.order_id!==order)throw new ApiError('INVALID_RESPONSE');this.update({bill,pending:Boolean(pending)});
   if(job){if(!id(job))throw new ApiError('INVALID_REQUEST');const result=await this.client.request('GET','/jobs/'+job,originalJobOf);this.context(epoch,owner);if(result.job_id!==job||result.order_id!==order)throw new ApiError('INVALID_RESPONSE');this.update({job:result});}
  }catch(e){if(epoch===this.epoch)this.update({error:errorText(e)});}finally{if(epoch===this.epoch)this.update({busy:false});}
 }
 private async command(operation:Pending['operation'],body:Decision|Cancel){
  if(this.state.busy||!this.order||!this.state.job)return;const epoch=this.epoch,owner=this.client.current()?.profile.user_id;if(!owner||owner!==this.order.buyer_id)throw new ApiError('FORBIDDEN');
  const candidate:Pending={profile:'TSM_COMPLETION_COMMAND_1',owner,order:this.order.order_id,job:this.state.job.job_id,operation,key:'pending_validation',body};pendingOf(JSON.stringify(candidate),owner);
  this.update({busy:true,error:''});
  try{let pending=this.readPending(owner);if(pending&&(pending.order!==candidate.order||pending.job!==candidate.job||pending.operation!==operation||JSON.stringify(pending.body)!==JSON.stringify(body)))throw new Error('存在待确认任务操作，请先恢复原操作。');
   if(!pending){const key=await newKey(this.random);this.context(epoch,owner);pending={...candidate,key};this.storage.set('completion-pending:'+owner,JSON.stringify(pending));}
   this.update({pending:true});await this.send(pending,epoch,owner);
  }catch(e){if(epoch===this.epoch)this.update({error:errorText(e)});}finally{if(epoch===this.epoch)this.update({busy:false});}
 }
 private async send(p:Pending,epoch:number,owner:string){this.context(epoch,owner);
  try{const ack=await this.client.request('POST','/jobs/'+p.job+(p.operation==='decision'?'/receipts':'/cancel'),ackOf,p.body,p.key);this.context(epoch,owner);
   const expected=p.operation==='cancel'?'CANCELED':(p.body as Decision).decision==='ACCEPT'?'ACCEPTED':'FAILED';if(ack.job_id!==p.job||ack.state!==expected||ack.version!==p.body.expected_job_version+1||(p.operation==='decision'?ack.delivery_id!==(p.body as Decision).delivery_id:ack.delivery_id!==undefined||ack.id!==p.job))throw new ApiError('INVALID_RESPONSE',200,true);
   if(this.storage.get('completion-pending:'+owner)===JSON.stringify(p))this.storage.remove('completion-pending:'+owner);this.update({pending:false,job:null,bill:null});
  }catch(e){this.context(epoch,owner);if(e instanceof ApiError&&e.status>=400&&e.status<500&&![401,403,429].includes(e.status)&&!e.uncertain){if(this.storage.get('completion-pending:'+owner)===JSON.stringify(p))this.storage.remove('completion-pending:'+owner);this.update({pending:false});}throw e;}
 }
 async recover(){if(this.state.busy||!this.order)return;const epoch=this.epoch,owner=this.client.current()?.profile.user_id;if(!owner||owner!==this.order.buyer_id)return;this.update({busy:true,error:''});try{const p=this.readPending(owner);if(!p)return;if(p.order!==this.order.order_id)throw new Error('请先打开原待确认操作所属订单。');this.update({pending:true});await this.send(p,epoch,owner);}catch(e){if(epoch===this.epoch)this.update({error:errorText(e)});}finally{if(epoch===this.epoch)this.update({busy:false});}}
 // Only a consumer that received and checked actual original output supplies
 // this receipt. Listing server metadata never fabricates local acceptance.
 async decide(receipt:LocalReviewReceipt,accept:boolean){const j=this.state.job;if(!j||receipt.profile_id!=='P11_LOCAL_RESULT_REVIEW_1'||receipt.job_id!==j.job_id)throw new ApiError('INVALID_REQUEST');const d=j.results.find(x=>x.delivery_id===receipt.delivery_id);if(!d||d.attempt_id!==j.current_attempt_id||d.revision!==receipt.revision||d.manifest_hash!==receipt.manifest_hash||d.output_digest!==receipt.output_digest||d.state!=='WAITING_BUYER')throw new ApiError('CONFLICT');return this.command('decision',{expected_job_version:j.version,delivery_id:d.delivery_id,revision:d.revision,manifest_hash:d.manifest_hash,reviewed_output_digest:receipt.output_digest,decision:accept?'ACCEPT':'REJECT'});}
 async cancel(){if(this.state.job)return this.command('cancel',{expected_job_version:this.state.job.version,reason_code:'BUYER_REQUEST'});}
 dispose(){this.epoch++;this.unsubscribe();this.order=null;this.state=initial();this.listeners.clear();}
}
