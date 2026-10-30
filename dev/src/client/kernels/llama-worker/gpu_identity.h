// Included inside the worker's private namespace after api is declared.
// CUDA Driver ABI declarations are limited to metadata calls; no CUDA context,
// memory allocation, kernel launch or device reset is requested here.
struct physical_gpu { std::string device, uuid, pci, name; uint64_t total = 0; };
std::string bounded_string(const char * p, size_t limit) {
    if (!p) throw fault{};
    size_t n=0;while(n<=limit && p[n]) ++n;
    if(!n || n>limit) throw fault{};
    return std::string(p,n);
}
std::string canonical_pci(const std::string & s) {
    if(s.size()!=12 && s.size()!=16) throw fault{};
    for(char c:s) if(!((c>='0'&&c<='9')||(c>='a'&&c<='f')||(c>='A'&&c<='F')||c==':'||c=='.')) throw fault{};
    unsigned domain=0,bus=0,dev=0,fn=0;int used=0;
    if(std::sscanf(s.c_str(),"%x:%x:%x.%x%n",&domain,&bus,&dev,&fn,&used)!=4 || used!=static_cast<int>(s.size()) || domain || bus>255 || dev>31 || fn>7) throw fault{};
    char out[17]{};std::snprintf(out,sizeof(out),"0000:%02x:%02x.%x",bus,dev,fn);return out;
}
void put_identity(bytes & b, const physical_gpu & i) {
    for(const auto * s:{&i.device,&i.uuid,&i.pci,&i.name}) putstr(b,*s);
    put32(b,static_cast<uint32_t>(i.total>>32));put32(b,static_cast<uint32_t>(i.total));
}
physical_gpu identify(api & a, ggml_backend_dev_t device) {
    ggml_backend_dev_props props{};a.device_props(device,&props);
    physical_gpu i;i.device=bounded_string(props.name,128);i.pci=canonical_pci(bounded_string(props.device_id,16));
    if(i.device.rfind("CUDA",0)!=0 || props.type!=GGML_BACKEND_DEVICE_TYPE_GPU) throw fault{};
#ifdef _WIN32
    struct driver_owner { HMODULE h;~driver_owner(){if(h) FreeLibrary(h);} } driver{LoadLibraryExW(L"nvcuda.dll",nullptr,LOAD_LIBRARY_SEARCH_SYSTEM32)};
    if(!driver.h) {startup_native_code=GetLastError();throw fault{};}
    using init_fn=int (WINAPI *)(unsigned);
    using by_pci_fn=int (WINAPI *)(int *,const char *);
    using pci_fn=int (WINAPI *)(char *,int,int);
    struct uuid_bytes { unsigned char data[16]; };
    using uuid_fn=int (WINAPI *)(uuid_bytes *,int);
    using name_fn=int (WINAPI *)(char *,int,int);
    using memory_fn=int (WINAPI *)(size_t *,int);
    auto init=symbol<init_fn>(driver.h,"cuInit");auto by_pci=symbol<by_pci_fn>(driver.h,"cuDeviceGetByPCIBusId");
    auto pci=symbol<pci_fn>(driver.h,"cuDeviceGetPCIBusId");auto uuid=symbol<uuid_fn>(driver.h,"cuDeviceGetUuid");
    auto uuid_v2=symbol<uuid_fn>(driver.h,"cuDeviceGetUuid_v2");auto name=symbol<name_fn>(driver.h,"cuDeviceGetName");
    auto memory=symbol<memory_fn>(driver.h,"cuDeviceTotalMem_v2");
    auto checked=[](int code){if(code){startup_native_code=static_cast<uint32_t>(code);throw fault{};}};
    checked(init(0));int dev=0;checked(by_pci(&dev,i.pci.c_str()));
    char bus[17]{},description[193]{};uuid_bytes first{},second{};size_t total=0;
    checked(pci(bus,sizeof(bus),dev));checked(uuid(&first,dev));checked(uuid_v2(&second,dev));
    // Reject MIG/partition identity differences; whole-board sharing only.
    if(std::memcmp(first.data,second.data,16) || canonical_pci(bounded_string(bus,16))!=i.pci) throw fault{};
    checked(name(description,sizeof(description),dev));checked(memory(&total,dev));
    i.name=bounded_string(description,192);i.total=total;
    static constexpr char hex[]="0123456789abcdef";i.uuid="GPU-";
    for(size_t n=0;n<16;++n){if(n==4||n==6||n==8||n==10)i.uuid+='-';i.uuid+=hex[second.data[n]>>4];i.uuid+=hex[second.data[n]&15];}
    if(i.total==0 || i.total>(uint64_t{1}<<40)) throw fault{};
    for(char c:i.name) if(c<32 || c>126) throw fault{};
    return i;
#else
    // No qualified system-driver lookup is shipped for Linux in this round.
    throw fault{};
#endif
}
