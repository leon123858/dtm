import {api, memberOperation} from '../src/api.js';
import {CREATE_TRIP, CREATE_ADDRESS, CREATE_RECORD, UPDATE_RECORD, GET_TRIP} from './graphql.js';

const member = api(process.env.DTM_E2E_ID_TOKEN);
const anonymous = api();
const BIND = `mutation($id:ID!,$address:ID){bind(tripId:$id,addressId:$address){bind(tripId:$id){tripId address{id statistics{totalExpense totalPrepaid}}}}}`;
const PRIVATE = `mutation($id:ID!,$private:Boolean!){updateTrip(tripId:$id,input:{private:$private}){id private}}`;
const UNBIND = `mutation($id:ID!){unbind(tripId:$id){bind(tripId:$id){tripId}}}`;

async function rejects(actor, query, variables, code) {
 const result = await actor.request(query, variables);
 expect(result.status).toBe(200);
 expect(result.body.errors[0].extensions.code).toBe(code);
}

test('binding preserves membership without an address and private records reject anonymous access by ID', async () => {
 const {id} = (await member.run(CREATE_TRIP, {input:{name:'私有旅程'}})).createTrip;
 let locked = false;
 try {
  expect((await member.run(`query($id:ID!){me{bind(tripId:$id){tripId}} trip(tripId:$id){private}}`,{id}))).toMatchObject({me:{bind:null},trip:{private:false}});
  await rejects(member, PRIVATE, {id,private:true}, 'FORBIDDEN');
  const address = (await anonymous.run(CREATE_ADDRESS,{tripId:id,input:{name:'成員'}})).createAddress;
  const old = {name:'支出',amount:50,time:'1700000000000',prePayAddressId:address.id,shouldPayAddressIds:[address.id],category:'NORMAL',extendPayMsg:[]};
  const record = (await anonymous.run(CREATE_RECORD,{tripId:id,input:old})).createRecord;
  const joined = await member.run(BIND,{id,address:address.id});
  expect(joined.bind.bind).toMatchObject({tripId:id,address:{id:address.id,statistics:{totalExpense:50,totalPrepaid:50}}});
  expect((await member.run(PRIVATE,{id,private:true})).updateTrip.private).toBe(true);
  locked = true;
  await rejects(anonymous,GET_TRIP,{tripId:id,haveHistory:true},'UNAUTHENTICATED');
  await rejects(anonymous,CREATE_RECORD,{tripId:id,input:old},'UNAUTHENTICATED');
  await rejects(anonymous,UPDATE_RECORD,{recordId:record.id,input:{old,new:{...old,name:'denied'}}},'UNAUTHENTICATED');
  expect((await member.run(GET_TRIP,{tripId:id})).trip.records[0].name).toBe('支出');
  expect((await member.run(BIND,{id})).bind.bind).toEqual({tripId:id,address:null});
  expect((await member.run(BIND,{id,address:null})).bind.bind.address).toBeNull();
  await rejects(member,UNBIND,{id},'CONFLICT');
  await member.run(PRIVATE,{id,private:false});locked = false;
  expect((await member.run(UNBIND,{id})).unbind.bind).toBeNull();
  expect((await member.run(UNBIND,{id})).unbind.bind).toBeNull();
  await anonymous.run(GET_TRIP,{tripId:id});
 } finally {
  if (locked) await member.run(PRIVATE,{id,private:false});
  await member.run(UNBIND,{id});
  await memberOperation(member,'unsubscribeTrip',id);
 }
});
