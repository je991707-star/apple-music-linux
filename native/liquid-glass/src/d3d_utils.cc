#include "d3d_utils.h"
#include <dxgi.h>
#include <cstdio>
#include <cstdlib>

namespace {

void LogAdapter(const char* how, IDXGIAdapter* adapter) {
    if (!std::getenv("ADAM_GLASS_DEBUG")) return;
    DXGI_ADAPTER_DESC d{};
    if (adapter && SUCCEEDED(adapter->GetDesc(&d))) {
        std::fprintf(stderr, "[liquid-glass] adapter (%s): %ls\n", how, d.Description);
    } else {
        std::fprintf(stderr, "[liquid-glass] adapter (%s)\n", how);
    }
}

bool HasOutput(IDXGIAdapter* adapter) {
    ComPtr<IDXGIOutput> out;
    return SUCCEEDED(adapter->EnumOutputs(0, &out));
}

}  // namespace

// Desktop Duplication only works on the adapter that owns the display output, so
// prefer a hardware adapter with an output attached, then any hardware adapter,
// then WARP (software).
HRESULT CreateD3DDevice(ComPtr<ID3D11Device>* device, ComPtr<ID3D11DeviceContext>* context) {
    const UINT flags = D3D11_CREATE_DEVICE_BGRA_SUPPORT;
    const D3D_FEATURE_LEVEL levels[] = { D3D_FEATURE_LEVEL_11_1, D3D_FEATURE_LEVEL_11_0 };

    ComPtr<IDXGIFactory1> factory;
    if (SUCCEEDED(CreateDXGIFactory1(IID_PPV_ARGS(&factory)))) {
        for (int pass = 0; pass < 2; ++pass) {  // pass 0: needs an output; pass 1: any
            for (UINT i = 0;; ++i) {
                ComPtr<IDXGIAdapter1> adapter;
                if (factory->EnumAdapters1(i, &adapter) == DXGI_ERROR_NOT_FOUND) break;
                DXGI_ADAPTER_DESC1 d{};
                if (FAILED(adapter->GetDesc1(&d)) || (d.Flags & DXGI_ADAPTER_FLAG_SOFTWARE)) continue;
                if (pass == 0 && !HasOutput(adapter.Get())) continue;
                HRESULT hr = D3D11CreateDevice(
                    adapter.Get(), D3D_DRIVER_TYPE_UNKNOWN, nullptr, flags,
                    levels, ARRAYSIZE(levels), D3D11_SDK_VERSION,
                    device->ReleaseAndGetAddressOf(), nullptr, context->ReleaseAndGetAddressOf());
                if (SUCCEEDED(hr)) { LogAdapter(pass == 0 ? "owns output" : "hardware", adapter.Get()); return hr; }
            }
        }
    }
    HRESULT hr = D3D11CreateDevice(
        nullptr, D3D_DRIVER_TYPE_HARDWARE, nullptr, flags,
        levels, ARRAYSIZE(levels), D3D11_SDK_VERSION,
        device->ReleaseAndGetAddressOf(), nullptr, context->ReleaseAndGetAddressOf());
    if (SUCCEEDED(hr)) { LogAdapter("default hardware", nullptr); return hr; }
    LogAdapter("WARP software", nullptr);
    return D3D11CreateDevice(
        nullptr, D3D_DRIVER_TYPE_WARP, nullptr, flags,
        levels, ARRAYSIZE(levels), D3D11_SDK_VERSION,
        device->ReleaseAndGetAddressOf(), nullptr, context->ReleaseAndGetAddressOf());
}
