import {api, memberOperation} from '../src/api.js';
const member=api(process.env.DTM_E2E_ID_TOKEN),anonymous=api();
const UPDATE=`mutation($input:UpdateMe!){updateMe(input:$input){id name email subMail}}`;
const CREATE=`mutation($id:ID!,$input:NewAddress!,$bind:Boolean!){createAddress(tripId:$id,input:$input,bindToMe:$bind){id name useNickname}}`;
const PATCH=`mutation($id:ID!,$aid:ID!,$input:NewAddress!){updateAddress(tripId:$id,addressId:$aid,input:$input){id name useNickname}}`;
test('profile, current names on historical records, and atomic create-and-bind retries',async()=>{
 const before=(await member.run('{me{id name email subMail}}')).me;
 const rejected=await anonymous.request(UPDATE,{input:{name:'denied'}});expect(rejected.body.errors[0].extensions.code).toBe('UNAUTHENTICATED');
 const profile=(await member.run(UPDATE,{input:{name:'小明',subMail:true}})).updateMe;expect(profile).toMatchObject({id:before.id,name:'小明',subMail:true});expect(profile.email).toBeTruthy();
 const id=(await member.run('mutation{createTrip(input:{name:"profile"}){id}}')).createTrip.id;
 try{
  const first=(await member.run(CREATE,{id,input:{name:'旅伴 A'},bind:true})).createAddress;expect(first).toMatchObject({name:'小明',useNickname:false});
  const retries=await Promise.all(Array.from({length:6},()=>member.run(CREATE,{id,input:{name:'ignored',useNickname:true},bind:true})));for(const r of retries)expect(r.createAddress).toEqual(first);
  const record=(await member.run(`mutation($id:ID!,$aid:ID!){createRecord(tripId:$id,input:{name:"meal",amount:30,prePayAddressId:$aid,shouldPayAddressIds:[$aid]}){id prePayAddress{name useNickname} shouldPayAddress{name useNickname}}}`,{id,aid:first.id})).createRecord;expect(record.prePayAddress).toEqual({name:'小明',useNickname:false});
  await member.run(UPDATE,{input:{name:'新名',subMail:false}});
  let trip=(await member.run(`query($id:ID!){trip(tripId:$id,haveHistory:true){addresses{id name useNickname} records{amount prePayAddress{id name useNickname} shouldPayAddress{id name useNickname}}}}`,{id})).trip;
  expect(trip.addresses).toEqual([{id:first.id,name:'新名',useNickname:false}]);expect(trip.records[0]).toMatchObject({amount:30,prePayAddress:{name:'新名',useNickname:false},shouldPayAddress:[{name:'新名',useNickname:false}]});
  expect((await member.run(PATCH,{id,aid:first.id,input:{useNickname:true}})).updateAddress).toEqual({...first,name:'旅伴 A',useNickname:true});
  expect((await member.run(PATCH,{id,aid:first.id,input:{name:null,useNickname:null}})).updateAddress.name).toBe('旅伴 A');
  expect((await member.run(PATCH,{id,aid:first.id,input:{name:'原名',useNickname:false}})).updateAddress.name).toBe('新名');
  const untouched=(await member.run(UPDATE,{input:{name:null,subMail:null}})).updateMe;expect(untouched).toMatchObject({name:'新名',subMail:false});
  await member.run(`mutation($id:ID!){unbind(tripId:$id){id name email subMail}}`,{id});
  expect((await anonymous.run(PATCH,{id,aid:first.id,input:{}})).updateAddress).toEqual({...first,name:'原名'});
 }finally{
  await member.run(UPDATE,{input:{name:before.name||'Member',subMail:before.subMail}});
  await member.run(`mutation($id:ID!){unbind(tripId:$id){id}}`,{id});await memberOperation(member,'unsubscribeTrip',id);
 }
});
