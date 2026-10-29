import {api, memberOperation} from '../src/api.js';
import {CREATE_TRIP, CREATE_ADDRESS, CREATE_RECORD, UPDATE_RECORD, GET_TRIP} from './graphql.js';

const member = api(process.env.DTM_E2E_ID_TOKEN);
const anonymous = api();
const BIND = `mutation($id:ID!,$address:ID){bind(tripId:$id,addressId:$address){id}}`;
const PRIVATE = `mutation($id:ID!,$private:Boolean!){updateTrip(tripId:$id,input:{private:$private}){id private}}`;
const UNBIND = `mutation($id:ID!){unbind(tripId:$id){id}}`;

async function rejects(actor, query, variables, code) {
 const result = await actor.request(query, variables);
 expect(result.status).toBe(200);
 expect(result.body.errors[0].extensions.code).toBe(code);
}

test('binding preserves membership without an address and private records reject anonymous access by ID', async () => {
 const {id} = (await member.run(CREATE_TRIP, {input:{name:'私有旅程'}})).createTrip;
 let locked = false;
 try {
  expect((await member.run(`query($id:ID!){trip(tripId:$id){private users{userId}}}`,{id}))).toMatchObject({trip:{private:false,users:[]}});
  await rejects(member, PRIVATE, {id,private:true}, 'FORBIDDEN');
  const address = (await anonymous.run(CREATE_ADDRESS,{tripId:id,input:{name:'成員'}})).createAddress;
  const old = {name:'支出',amount:50,time:'1700000000000',prePayAddressId:address.id,shouldPayAddressIds:[address.id],category:'NORMAL',extendPayMsg:[]};
  const record = (await anonymous.run(CREATE_RECORD,{tripId:id,input:old})).createRecord;
  const joined = await member.run(BIND,{id,address:address.id});
  expect((await state(id)).addresses).toContainEqual(expect.objectContaining({id:address.id,bind:{userId:joined.bind.id},statistics:{totalExpense:50,totalPrepaid:50}}));
  expect((await member.run(PRIVATE,{id,private:true})).updateTrip.private).toBe(true);
  locked = true;
  await rejects(anonymous,GET_TRIP,{tripId:id,haveHistory:true},'UNAUTHENTICATED');
  await rejects(anonymous,CREATE_RECORD,{tripId:id,input:old},'UNAUTHENTICATED');
  await rejects(anonymous,UPDATE_RECORD,{recordId:record.id,input:{old,new:{...old,name:'denied'}}},'UNAUTHENTICATED');
  expect((await member.run(GET_TRIP,{tripId:id})).trip.records[0].name).toBe('支出');
  await member.run(BIND,{id});
  expect((await state(id)).users).toContainEqual({userId:joined.bind.id});
  expect((await state(id)).addresses.every(a => a.bind === null)).toBe(true);
  await member.run(BIND,{id,address:null});
  expect((await state(id)).users).toContainEqual({userId:joined.bind.id});
  expect((await state(id)).addresses.every(a => a.bind === null)).toBe(true);
  await rejects(member,UNBIND,{id},'CONFLICT');
  await member.run(PRIVATE,{id,private:false});locked = false;
  await member.run(UNBIND,{id});
  expect((await state(id)).users).toEqual([]);
  await member.run(UNBIND,{id});
  expect((await state(id)).users).toEqual([]);
  await anonymous.run(GET_TRIP,{tripId:id});
 } finally {
  if (locked) await member.run(PRIVATE,{id,private:false});
  await member.run(UNBIND,{id});
  await memberOperation(member,'unsubscribeTrip',id);
 }
});

async function state(id) {
 return (await member.run(`query($id:ID!){trip(tripId:$id){users{userId} addresses{id bind{userId} statistics{totalExpense totalPrepaid}}}}`,{id})).trip;
}

test('User.bind is no longer available', async () => {
 const result = await member.request('query { me { bind(tripId:"removed") { tripId } } }', {});
 expect(result.body.errors[0].extensions.code).toBe('GRAPHQL_VALIDATION_FAILED');
});
